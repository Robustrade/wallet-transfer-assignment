package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

type TransferService struct {
	store repository.Store
}

func NewTransferService(store repository.Store) *TransferService {
	return &TransferService{store: store}
}

type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Currency       string
}

type TransferResult struct {
	TransferID     string  `json:"transferId"`
	IdempotencyKey string  `json:"idempotencyKey"`
	Status         string  `json:"status"`
	FromWalletID   string  `json:"fromWalletId"`
	ToWalletID     string  `json:"toWalletId"`
	Amount         int64   `json:"amount"`
	Currency       string  `json:"currency"`
	FailureReason  *string `json:"failureReason"`
	CreatedAt      string  `json:"createdAt"`
}

type CreateTransferResult struct {
	Transfer TransferResult
	Replayed bool
}

func (service *TransferService) CreateTransfer(ctx context.Context, request CreateTransferRequest) (CreateTransferResult, error) {
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.FromWalletID = strings.TrimSpace(request.FromWalletID)
	request.ToWalletID = strings.TrimSpace(request.ToWalletID)

	money, err := domain.NewMoney(request.Amount, request.Currency)
	if err != nil {
		return CreateTransferResult{}, err
	}
	request.Currency = money.Currency

	pendingTransfer, err := domain.NewTransfer(request.IdempotencyKey, request.FromWalletID, request.ToWalletID, money)
	if err != nil {
		return CreateTransferResult{}, err
	}

	fingerprint := requestFingerprint(request)
	var transferID string
	var replayed *TransferResult

	err = service.store.WithTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		inserted, err := tx.InsertIdempotencyRecord(ctx, request.IdempotencyKey, fingerprint)
		if err != nil {
			return err
		}
		if !inserted {
			record, err := tx.GetIdempotencyRecord(ctx, request.IdempotencyKey)
			if err != nil {
				return err
			}
			if record.RequestFingerprint != fingerprint {
				return domain.ErrIdempotencyConflict
			}
			if record.Status == domain.IdempotencyStatusCompleted {
				result, err := decodeTransferResult(record.ResponseSnapshot)
				if err != nil {
					return err
				}
				replayed = &result
				return nil
			}
			if record.TransferID == nil {
				return domain.ErrRequestInProgress
			}
			transferID = *record.TransferID
			return nil
		}

		fromWallet, err := tx.GetWallet(ctx, request.FromWalletID)
		if err != nil {
			return err
		}
		toWallet, err := tx.GetWallet(ctx, request.ToWalletID)
		if err != nil {
			return err
		}
		if err := validateWalletsForNewTransfer(fromWallet, toWallet, money.Currency); err != nil {
			return err
		}

		createdTransfer, err := tx.InsertPendingTransfer(ctx, pendingTransfer)
		if err != nil {
			return err
		}
		if err := tx.SetIdempotencyTransfer(ctx, request.IdempotencyKey, createdTransfer.ID); err != nil {
			return err
		}
		transferID = createdTransfer.ID
		return nil
	})
	if err != nil {
		return CreateTransferResult{}, err
	}
	if replayed != nil {
		return CreateTransferResult{Transfer: *replayed, Replayed: true}, nil
	}

	result, err := service.processTransfer(ctx, transferID)
	if err != nil {
		return CreateTransferResult{}, err
	}
	return CreateTransferResult{Transfer: result}, nil
}

func (service *TransferService) GetTransfer(ctx context.Context, id string) (TransferResult, error) {
	transfer, err := service.store.GetTransfer(ctx, strings.TrimSpace(id))
	if err != nil {
		return TransferResult{}, err
	}
	return buildTransferResult(transfer), nil
}

func (service *TransferService) processTransfer(ctx context.Context, transferID string) (TransferResult, error) {
	var result TransferResult

	err := service.store.WithTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		transfer, err := tx.LockTransferForProcessing(ctx, transferID)
		if err != nil {
			return err
		}
		if transfer.IsTerminal() {
			result = buildTransferResult(transfer)
			snapshot, err := json.Marshal(result)
			if err != nil {
				return err
			}
			return tx.CompleteIdempotencyRecord(ctx, transfer.IdempotencyKey, transfer.ID, snapshot)
		}

		fromWallet, toWallet, err := tx.LockWalletsForTransfer(ctx, transfer.FromWalletID, transfer.ToWalletID)
		if err != nil {
			return err
		}

		money, err := domain.NewMoney(transfer.Amount, transfer.Currency)
		if err != nil {
			return err
		}

		if err := toWallet.CanCredit(money.Currency); err != nil {
			if markErr := markTransferFailure(&transfer, failureReason(err)); markErr != nil {
				return markErr
			}
		} else if err := fromWallet.CanDebit(money); err != nil {
			if markErr := markTransferFailure(&transfer, failureReason(err)); markErr != nil {
				return markErr
			}
		} else {
			fromWallet.Balance -= money.Amount
			toWallet.Balance += money.Amount

			debit, err := domain.NewDebit(transfer.ID, fromWallet.ID, money, fromWallet.Balance)
			if err != nil {
				return err
			}
			credit, err := domain.NewCredit(transfer.ID, toWallet.ID, money, toWallet.Balance)
			if err != nil {
				return err
			}
			if err := tx.InsertLedgerEntries(ctx, []domain.LedgerEntry{debit, credit}); err != nil {
				return err
			}
			if err := tx.UpdateWalletBalances(ctx, fromWallet, toWallet); err != nil {
				return err
			}
			if err := transfer.MarkProcessed(); err != nil {
				return err
			}
		}

		if err := tx.CompleteTransfer(ctx, transfer); err != nil {
			return err
		}
		result = buildTransferResult(transfer)
		snapshot, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return tx.CompleteIdempotencyRecord(ctx, transfer.IdempotencyKey, transfer.ID, snapshot)
	})
	if err != nil {
		return TransferResult{}, err
	}
	return result, nil
}

func validateWalletsForNewTransfer(fromWallet domain.Wallet, toWallet domain.Wallet, currency string) error {
	if fromWallet.Status != domain.WalletStatusActive || toWallet.Status != domain.WalletStatusActive {
		return domain.ErrWalletNotActive
	}
	if fromWallet.Currency != currency || toWallet.Currency != currency {
		return domain.ErrCurrencyMismatch
	}
	return nil
}

func markTransferFailure(transfer *domain.Transfer, reason string) error {
	return transfer.MarkFailed(reason)
}

func failureReason(err error) string {
	switch {
	case errors.Is(err, domain.ErrWalletNotActive):
		return domain.FailureWalletNotActive
	case errors.Is(err, domain.ErrInsufficientFunds):
		return domain.FailureInsufficientFunds
	case errors.Is(err, domain.ErrCurrencyMismatch):
		return domain.FailureCurrencyMismatch
	default:
		return "TRANSFER_FAILED"
	}
}

func requestFingerprint(request CreateTransferRequest) string {
	raw := fmt.Sprintf("%s|%s|%d|%s", request.FromWalletID, request.ToWalletID, request.Amount, domain.NormalizeCurrency(request.Currency))
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func buildTransferResult(transfer domain.Transfer) TransferResult {
	return TransferResult{
		TransferID:     transfer.ID,
		IdempotencyKey: transfer.IdempotencyKey,
		Status:         string(transfer.Status),
		FromWalletID:   transfer.FromWalletID,
		ToWalletID:     transfer.ToWalletID,
		Amount:         transfer.Amount,
		Currency:       transfer.Currency,
		FailureReason:  transfer.FailureReason,
		CreatedAt:      formatTime(transfer.CreatedAt),
	}
}

func decodeTransferResult(snapshot []byte) (TransferResult, error) {
	if len(snapshot) == 0 {
		return TransferResult{}, domain.ErrRequestInProgress
	}
	var result TransferResult
	if err := json.Unmarshal(snapshot, &result); err != nil {
		return TransferResult{}, err
	}
	return result, nil
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
