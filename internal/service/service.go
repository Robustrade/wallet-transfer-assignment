package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"wallet-transfer-assignment/internal/domain"
)

var (
	ErrIdempotencyConflict = errors.New("idempotency key was previously used with a different request")
	ErrIdempotencyInFlight = errors.New("idempotency key is currently being processed")
	ErrInvalidTransfer     = errors.New("invalid transfer request")
)

type Wallet struct {
	ID      string
	Balance int64
}

type IdempotencyRecord struct {
	Key          string
	RequestHash  string
	ResponseCode int
	ResponseBody []byte
	Completed    bool
}

type TransferTx interface {
	FindOrCreateIdempotency(ctx context.Context, key string, requestHash string) (IdempotencyRecord, bool, error)
	CompleteIdempotency(ctx context.Context, key string, outcome domain.Outcome) error
	GetWallet(ctx context.Context, walletID string) (Wallet, bool, error)
	CreateTransfer(ctx context.Context, transfer domain.Transfer) error
	SetTransferState(ctx context.Context, transferID string, state domain.TransferState, failureReason string) error
	DebitWallet(ctx context.Context, walletID string, amount int64) (int64, bool, error)
	CreditWallet(ctx context.Context, walletID string, amount int64) (int64, error)
	CreateLedgerEntries(ctx context.Context, entries []domain.LedgerEntry) error
}

type Repository interface {
	WithinTx(ctx context.Context, fn func(context.Context, TransferTx) error) error
}

type Service struct {
	repo Repository
}

func New(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateTransfer(ctx context.Context, cmd domain.TransferCommand) (domain.Outcome, error) {
	if err := validateCommand(cmd); err != nil {
		return domain.Outcome{}, err
	}

	requestHash, err := requestHash(cmd)
	if err != nil {
		return domain.Outcome{}, err
	}

	var outcome domain.Outcome
	err = s.repo.WithinTx(ctx, func(ctx context.Context, tx TransferTx) error {
		record, created, err := tx.FindOrCreateIdempotency(ctx, cmd.IdempotencyKey, requestHash)
		if err != nil {
			return err
		}
		if record.RequestHash != requestHash {
			return ErrIdempotencyConflict
		}
		if !created && record.Completed {
			replayed, err := decodeOutcome(record.ResponseCode, record.ResponseBody)
			if err != nil {
				return err
			}
			outcome = replayed
			return nil
		}
		if !created {
			return ErrIdempotencyInFlight
		}

		outcome, err = s.processTransfer(ctx, tx, cmd)
		if err != nil {
			return err
		}
		return tx.CompleteIdempotency(ctx, cmd.IdempotencyKey, outcome)
	})
	if err != nil {
		return domain.Outcome{}, err
	}

	return outcome, nil
}

func (s *Service) processTransfer(ctx context.Context, tx TransferTx, cmd domain.TransferCommand) (domain.Outcome, error) {
	if _, found, err := tx.GetWallet(ctx, cmd.FromWalletID); err != nil {
		return domain.Outcome{}, err
	} else if !found {
		return errorOutcome(http.StatusNotFound, "source wallet not found"), nil
	}

	if _, found, err := tx.GetWallet(ctx, cmd.ToWalletID); err != nil {
		return domain.Outcome{}, err
	} else if !found {
		return errorOutcome(http.StatusNotFound, "destination wallet not found"), nil
	}

	transferID, err := newID("tr")
	if err != nil {
		return domain.Outcome{}, err
	}
	transfer := domain.Transfer{
		ID:             transferID,
		IdempotencyKey: cmd.IdempotencyKey,
		FromWalletID:   cmd.FromWalletID,
		ToWalletID:     cmd.ToWalletID,
		Amount:         cmd.Amount,
		State:          domain.TransferPending,
	}
	if err := tx.CreateTransfer(ctx, transfer); err != nil {
		return domain.Outcome{}, err
	}

	fromBalance, debited, err := tx.DebitWallet(ctx, cmd.FromWalletID, cmd.Amount)
	if err != nil {
		return domain.Outcome{}, err
	}
	if !debited {
		transfer.State = domain.TransferFailed
		transfer.FailureReason = "insufficient funds"
		if err := tx.SetTransferState(ctx, transfer.ID, transfer.State, transfer.FailureReason); err != nil {
			return domain.Outcome{}, err
		}
		return domain.Outcome{
			StatusCode: http.StatusUnprocessableEntity,
			Body: domain.APIResponse{Data: &domain.TransferResponse{
				Transfer: transfer,
			}},
		}, nil
	}

	toBalance, err := tx.CreditWallet(ctx, cmd.ToWalletID, cmd.Amount)
	if err != nil {
		return domain.Outcome{}, err
	}

	debitID, err := newID("le")
	if err != nil {
		return domain.Outcome{}, err
	}
	creditID, err := newID("le")
	if err != nil {
		return domain.Outcome{}, err
	}
	entries := []domain.LedgerEntry{
		{ID: debitID, WalletID: cmd.FromWalletID, TransferID: transfer.ID, Type: domain.LedgerDebit, Amount: cmd.Amount},
		{ID: creditID, WalletID: cmd.ToWalletID, TransferID: transfer.ID, Type: domain.LedgerCredit, Amount: cmd.Amount},
	}
	if err := tx.CreateLedgerEntries(ctx, entries); err != nil {
		return domain.Outcome{}, err
	}

	transfer.State = domain.TransferProcessed
	if err := tx.SetTransferState(ctx, transfer.ID, transfer.State, ""); err != nil {
		return domain.Outcome{}, err
	}

	return domain.Outcome{
		StatusCode: http.StatusCreated,
		Body: domain.APIResponse{Data: &domain.TransferResponse{
			Transfer:      transfer,
			LedgerEntries: entries,
			FromBalance:   &fromBalance,
			ToBalance:     &toBalance,
		}},
	}, nil
}

func validateCommand(cmd domain.TransferCommand) error {
	switch {
	case cmd.IdempotencyKey == "":
		return fmt.Errorf("%w: idempotencyKey is required", ErrInvalidTransfer)
	case cmd.FromWalletID == "":
		return fmt.Errorf("%w: fromWalletId is required", ErrInvalidTransfer)
	case cmd.ToWalletID == "":
		return fmt.Errorf("%w: toWalletId is required", ErrInvalidTransfer)
	case cmd.FromWalletID == cmd.ToWalletID:
		return fmt.Errorf("%w: fromWalletId and toWalletId must differ", ErrInvalidTransfer)
	case cmd.Amount <= 0:
		return fmt.Errorf("%w: amount must be positive", ErrInvalidTransfer)
	default:
		return nil
	}
}

func requestHash(cmd domain.TransferCommand) (string, error) {
	payload := struct {
		FromWalletID string `json:"fromWalletId"`
		ToWalletID   string `json:"toWalletId"`
		Amount       int64  `json:"amount"`
	}{
		FromWalletID: cmd.FromWalletID,
		ToWalletID:   cmd.ToWalletID,
		Amount:       cmd.Amount,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func decodeOutcome(statusCode int, body []byte) (domain.Outcome, error) {
	var response domain.APIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return domain.Outcome{}, err
	}
	return domain.Outcome{StatusCode: statusCode, Body: response}, nil
}

func errorOutcome(status int, message string) domain.Outcome {
	return domain.Outcome{
		StatusCode: status,
		Body:       domain.APIResponse{Error: message},
	}
}

func newID(prefix string) (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(bytes[:]), nil
}
