package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/vardaanmittal09/wallet-transfer-assignment/internal/domain"
	"github.com/vardaanmittal09/wallet-transfer-assignment/internal/port"
)

type CreateTransferInput struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type TransferResult struct {
	ID             string                       `json:"id"`
	IdempotencyKey string                       `json:"idempotencyKey"`
	FromWalletID   string                       `json:"fromWalletId"`
	ToWalletID     string                       `json:"toWalletId"`
	Amount         int64                        `json:"amount"`
	State          domain.TransferState         `json:"state"`
	FailureReason  string                       `json:"failureReason,omitempty"`
	LedgerEntries  []domain.LedgerEntry         `json:"ledgerEntries,omitempty"`
	WalletBalances map[string]int64            `json:"walletBalances,omitempty"`
	Replay         bool                         `json:"replay"`
}

type TransferService struct {
	store port.TransferStore
}

func NewTransferService(store port.TransferStore) *TransferService {
	return &TransferService{store: store}
}

func (s *TransferService) CreateTransfer(ctx context.Context, input CreateTransferInput) (TransferResult, error) {
	input = normalize(input)
	if err := validate(input); err != nil {
		return TransferResult{}, err
	}

	requestHash, err := hashRequest(input)
	if err != nil {
		return TransferResult{}, err
	}

	var result TransferResult
	err = s.store.WithinTransfer(ctx, func(ctx context.Context, tx port.TransferTx) error {
		record, exists, err := tx.GetIdempotencyRecord(ctx, input.IdempotencyKey)
		if err != nil {
			return err
		}
		if exists {
			if record.RequestHash != requestHash {
				return domain.ErrIdempotencyConflict
			}
			transfer, err := tx.GetTransfer(ctx, record.TransferID)
			if err != nil {
				return err
			}
			ledger, err := tx.GetLedgerEntries(ctx, record.TransferID)
			if err != nil {
				return err
			}
			result = transferResult(transfer, ledger, nil, true)
			return nil
		}

		transfer := domain.Transfer{
			ID:             newID("tr"),
			IdempotencyKey: input.IdempotencyKey,
			FromWalletID:   input.FromWalletID,
			ToWalletID:     input.ToWalletID,
			Amount:         input.Amount,
			State:          domain.TransferPending,
		}
		if err := tx.InsertTransfer(ctx, transfer); err != nil {
			return err
		}
		if err := tx.InsertIdempotencyRecord(ctx, domain.IdempotencyRecord{
			Key:         input.IdempotencyKey,
			RequestHash: requestHash,
			TransferID:  transfer.ID,
		}); err != nil {
			return err
		}

		fromWallet, err := tx.GetWallet(ctx, input.FromWalletID)
		if err != nil {
			if errors.Is(err, domain.ErrWalletNotFound) {
				return failTransfer(ctx, tx, transfer, "source wallet not found", &result)
			}
			return err
		}
		toWallet, err := tx.GetWallet(ctx, input.ToWalletID)
		if err != nil {
			if errors.Is(err, domain.ErrWalletNotFound) {
				return failTransfer(ctx, tx, transfer, "destination wallet not found", &result)
			}
			return err
		}

		ok, err := tx.DebitWallet(ctx, input.FromWalletID, input.Amount)
		if err != nil {
			return err
		}
		if !ok {
			return failTransfer(ctx, tx, transfer, domain.ErrInsufficientFunds.Error(), &result)
		}
		if err := tx.CreditWallet(ctx, input.ToWalletID, input.Amount); err != nil {
			return err
		}

		debit := domain.LedgerEntry{
			WalletID:   input.FromWalletID,
			TransferID: transfer.ID,
			Type:       domain.LedgerDebit,
			Amount:     input.Amount,
		}
		credit := domain.LedgerEntry{
			WalletID:   input.ToWalletID,
			TransferID: transfer.ID,
			Type:       domain.LedgerCredit,
			Amount:     input.Amount,
		}
		if err := tx.InsertLedgerEntry(ctx, debit); err != nil {
			return err
		}
		if err := tx.InsertLedgerEntry(ctx, credit); err != nil {
			return err
		}
		if err := tx.MarkTransferProcessed(ctx, transfer.ID); err != nil {
			return err
		}

		ledger, err := tx.GetLedgerEntries(ctx, transfer.ID)
		if err != nil {
			return err
		}
		balances := map[string]int64{
			input.FromWalletID: fromWallet.Balance - input.Amount,
			input.ToWalletID:   toWallet.Balance + input.Amount,
		}
		transfer.State = domain.TransferProcessed
		result = transferResult(transfer, ledger, balances, false)
		return nil
	})

	return result, err
}

func (s *TransferService) GetWallet(ctx context.Context, walletID string) (domain.Wallet, error) {
	return s.store.GetWallet(ctx, walletID)
}

func (s *TransferService) GetTransfer(ctx context.Context, transferID string) (TransferResult, error) {
	transfer, err := s.store.GetTransfer(ctx, transferID)
	if err != nil {
		return TransferResult{}, err
	}
	ledger, err := s.store.GetLedgerEntries(ctx, transferID)
	if err != nil {
		return TransferResult{}, err
	}
	return transferResult(transfer, ledger, nil, false), nil
}

func normalize(input CreateTransferInput) CreateTransferInput {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.FromWalletID = strings.TrimSpace(input.FromWalletID)
	input.ToWalletID = strings.TrimSpace(input.ToWalletID)
	return input
}

func validate(input CreateTransferInput) error {
	if input.IdempotencyKey == "" {
		return domain.ErrIdempotencyKeyRequired
	}
	if input.FromWalletID == "" || input.ToWalletID == "" {
		return domain.ErrWalletIDRequired
	}
	if input.FromWalletID == input.ToWalletID {
		return domain.ErrSameWalletTransfer
	}
	if input.Amount <= 0 {
		return domain.ErrAmountMustBePositive
	}
	return nil
}

func hashRequest(input CreateTransferInput) (string, error) {
	payload := struct {
		FromWalletID string `json:"fromWalletId"`
		ToWalletID   string `json:"toWalletId"`
		Amount       int64  `json:"amount"`
	}{
		FromWalletID: input.FromWalletID,
		ToWalletID:   input.ToWalletID,
		Amount:       input.Amount,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func failTransfer(ctx context.Context, tx port.TransferTx, transfer domain.Transfer, reason string, result *TransferResult) error {
	if err := tx.MarkTransferFailed(ctx, transfer.ID, reason); err != nil {
		return err
	}
	transfer.State = domain.TransferFailed
	transfer.FailureReason = reason
	*result = transferResult(transfer, nil, nil, false)
	return nil
}

func transferResult(transfer domain.Transfer, ledger []domain.LedgerEntry, balances map[string]int64, replay bool) TransferResult {
	return TransferResult{
		ID:             transfer.ID,
		IdempotencyKey: transfer.IdempotencyKey,
		FromWalletID:   transfer.FromWalletID,
		ToWalletID:     transfer.ToWalletID,
		Amount:         transfer.Amount,
		State:          transfer.State,
		FailureReason:  transfer.FailureReason,
		LedgerEntries:  ledger,
		WalletBalances: balances,
		Replay:         replay,
	}
}

func newID(prefix string) string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(fmt.Sprintf("generate id: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(bytes[:])
}
