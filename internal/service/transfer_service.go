package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CreateTransferRequest is the input payload for initiating a wallet transfer.
type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"` // In minor units (cents)
	Currency       string `json:"currency,omitempty"`
}

// TransferResponse is the standard response returned for transfer operations.
type TransferResponse struct {
	TransferID     string                `json:"transferId"`
	IdempotencyKey string                `json:"idempotencyKey"`
	FromWalletID   string                `json:"fromWalletId"`
	ToWalletID     string                `json:"toWalletId"`
	Amount         int64                 `json:"amount"`
	Currency       string                `json:"currency"`
	Status         domain.TransferStatus `json:"status"`
	FailureReason  string                `json:"failureReason,omitempty"`
	CreatedAt      time.Time             `json:"createdAt"`
	IsReplay       bool                  `json:"-"` // Internal flag indicating replayed cached response
}

// TransferService defines business operations for executing and querying transfers.
type TransferService interface {
	ExecuteTransfer(ctx context.Context, req CreateTransferRequest) (*TransferResponse, error)
	GetTransfer(ctx context.Context, id string) (*domain.Transfer, error)
	GetTransferLedger(ctx context.Context, transferID string) ([]*domain.LedgerEntry, error)
}

type transferService struct {
	repo repository.Repository
}

// NewTransferService constructs a TransferService.
func NewTransferService(repo repository.Repository) TransferService {
	return &transferService{repo: repo}
}

// ExecuteTransfer coordinates the idempotent transfer lifecycle inside an atomic database transaction.
func (s *transferService) ExecuteTransfer(ctx context.Context, req CreateTransferRequest) (*TransferResponse, error) {
	// 1. Domain validations
	if req.IdempotencyKey == "" {
		return nil, domain.ErrMissingIdempotencyKey
	}
	if req.FromWalletID == "" || req.ToWalletID == "" {
		return nil, fmt.Errorf("fromWalletId and toWalletId must not be empty")
	}
	if req.FromWalletID == req.ToWalletID {
		return nil, domain.ErrSameWalletTransfer
	}
	if req.Amount <= 0 {
		return nil, domain.ErrInvalidAmount
	}

	// 2. Hash payload for idempotency verification
	canonicalPayload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize transfer request: %w", err)
	}
	reqHash := domain.HashPayload(canonicalPayload)

	// 3. Fast-path check: Existing Idempotency Record
	existingRecord, err := s.repo.Idempotency().Get(ctx, req.IdempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup idempotency record: %w", err)
	}

	if existingRecord != nil {
		// Verify payload match
		if existingRecord.RequestHash != reqHash {
			return nil, domain.ErrIdempotencyPayloadMismatch
		}

		if existingRecord.Status == domain.IdempotencyStatusCompleted || existingRecord.Status == domain.IdempotencyStatusFailed {
			var cachedResp TransferResponse
			if err := json.Unmarshal([]byte(existingRecord.ResponseBody), &cachedResp); err == nil {
				cachedResp.IsReplay = true
				if existingRecord.Status == domain.IdempotencyStatusFailed {
					switch cachedResp.FailureReason {
					case domain.ErrInsufficientBalance.Error():
						return &cachedResp, domain.ErrInsufficientBalance
					case domain.ErrCurrencyMismatch.Error():
						return &cachedResp, domain.ErrCurrencyMismatch
					case domain.ErrWalletInactive.Error():
						return &cachedResp, domain.ErrWalletInactive
					case domain.ErrSourceWalletNotFound.Error():
						return &cachedResp, domain.ErrSourceWalletNotFound
					case domain.ErrDestinationWalletNotFound.Error():
						return &cachedResp, domain.ErrDestinationWalletNotFound
					case domain.ErrWalletNotFound.Error():
						return &cachedResp, domain.ErrWalletNotFound
					default:
						return &cachedResp, errors.New(cachedResp.FailureReason)
					}
				}
				return &cachedResp, nil
			}
		}

		if existingRecord.Status == domain.IdempotencyStatusPending {
			return nil, domain.ErrIdempotencyConflict
		}
	}

	// 4. Execute atomic transfer transaction
	var finalResponse TransferResponse
	var txErr error

	err = s.repo.ExecuteInTransaction(ctx, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		transferID := "tr_" + uuid.New().String()

		// A. Record idempotency record as PENDING inside transaction
		idempotencyRec := &domain.IdempotencyRecord{
			Key:         req.IdempotencyKey,
			RequestHash: reqHash,
			Status:      domain.IdempotencyStatusPending,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := s.repo.Idempotency().Create(ctx, tx, idempotencyRec); err != nil {
			// If already created by concurrent request, this will fail on unique constraint
			return domain.ErrIdempotencyConflict
		}

		// B. Initial Transfer record in PENDING state
		transfer := &domain.Transfer{
			ID:             transferID,
			IdempotencyKey: req.IdempotencyKey,
			FromWalletID:   req.FromWalletID,
			ToWalletID:     req.ToWalletID,
			Amount:         req.Amount,
			Currency:       req.Currency,
			Status:         domain.TransferStatusPending,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := s.repo.Transfers().Create(ctx, tx, transfer); err != nil {
			return err
		}

		// C. Acquire row locks in deterministic order (lexicographically by ID)
		fromWallet, toWallet, err := s.repo.Wallets().GetPairForUpdate(ctx, tx, req.FromWalletID, req.ToWalletID)
		if err != nil {
			// Persist expected not-found domain errors for idempotent replay
			if errors.Is(err, domain.ErrSourceWalletNotFound) ||
				errors.Is(err, domain.ErrDestinationWalletNotFound) ||
				errors.Is(err, domain.ErrWalletNotFound) {
				transfer.Status = domain.TransferStatusFailed
				transfer.FailureReason = err.Error()
				_ = s.repo.Transfers().Update(ctx, tx, transfer)
				failedResp := TransferResponse{
					TransferID:     transferID,
					IdempotencyKey: req.IdempotencyKey,
					FromWalletID:   req.FromWalletID,
					ToWalletID:     req.ToWalletID,
					Amount:         req.Amount,
					Currency:       req.Currency,
					Status:         domain.TransferStatusFailed,
					FailureReason:  transfer.FailureReason,
					CreatedAt:      now,
				}
				respBytes, _ := json.Marshal(failedResp)
				idempotencyRec.Status = domain.IdempotencyStatusFailed
				idempotencyRec.ResponseCode = 404
				idempotencyRec.ResponseBody = string(respBytes)
				_ = s.repo.Idempotency().Update(ctx, tx, idempotencyRec)
				finalResponse = failedResp
				txErr = err
				return nil
			}
			// Unexpected database failure or validation error (e.g. same wallet) rolls back
			return err
		}
		// D. Wallet status validations
		if !fromWallet.IsActive() || !toWallet.IsActive() {
			transfer.Status = domain.TransferStatusFailed
			transfer.FailureReason = domain.ErrWalletInactive.Error()
			_ = s.repo.Transfers().Update(ctx, tx, transfer)

			failedResp := TransferResponse{
				TransferID:     transferID,
				IdempotencyKey: req.IdempotencyKey,
				FromWalletID:   req.FromWalletID,
				ToWalletID:     req.ToWalletID,
				Amount:         req.Amount,
				Currency:       req.Currency,
				Status:         domain.TransferStatusFailed,
				FailureReason:  transfer.FailureReason,
				CreatedAt:      now,
			}
			respBytes, _ := json.Marshal(failedResp)
			idempotencyRec.Status = domain.IdempotencyStatusFailed
			idempotencyRec.ResponseCode = 422
			idempotencyRec.ResponseBody = string(respBytes)
			if err := s.repo.Idempotency().Update(ctx, tx, idempotencyRec); err != nil {
				return fmt.Errorf("failed to finalize failed idempotency record: %w", err)
			}
			finalResponse = failedResp
			txErr = domain.ErrWalletInactive
			return nil
		}

		// E. Currency validations: reject cross-currency transfers and request currency mismatches
		if fromWallet.Currency != toWallet.Currency || (req.Currency != "" && req.Currency != fromWallet.Currency) {
			transfer.Status = domain.TransferStatusFailed
			transfer.FailureReason = domain.ErrCurrencyMismatch.Error()
			_ = s.repo.Transfers().Update(ctx, tx, transfer)

			failedResp := TransferResponse{
				TransferID:     transferID,
				IdempotencyKey: req.IdempotencyKey,
				FromWalletID:   req.FromWalletID,
				ToWalletID:     req.ToWalletID,
				Amount:         req.Amount,
				Currency:       req.Currency,
				Status:         domain.TransferStatusFailed,
				FailureReason:  transfer.FailureReason,
				CreatedAt:      now,
			}
			respBytes, _ := json.Marshal(failedResp)
			idempotencyRec.Status = domain.IdempotencyStatusFailed
			idempotencyRec.ResponseCode = 422
			idempotencyRec.ResponseBody = string(respBytes)
			if err := s.repo.Idempotency().Update(ctx, tx, idempotencyRec); err != nil {
				return fmt.Errorf("failed to finalize failed idempotency record: %w", err)
			}

			finalResponse = failedResp
			txErr = domain.ErrCurrencyMismatch
			return nil
		}

		if req.Currency == "" {
			req.Currency = fromWallet.Currency
			transfer.Currency = fromWallet.Currency
		}

		// F. Check balance
		if fromWallet.Balance < req.Amount {
			transfer.Status = domain.TransferStatusFailed
			transfer.FailureReason = domain.ErrInsufficientBalance.Error()
			if err := s.repo.Transfers().Update(ctx, tx, transfer); err != nil {
				return fmt.Errorf("failed to mark transfer as failed: %w", err)
			}

			failedResp := TransferResponse{
				TransferID:     transferID,
				IdempotencyKey: req.IdempotencyKey,
				FromWalletID:   req.FromWalletID,
				ToWalletID:     req.ToWalletID,
				Amount:         req.Amount,
				Currency:       req.Currency,
				Status:         domain.TransferStatusFailed,
				FailureReason:  transfer.FailureReason,
				CreatedAt:      now,
			}
			respBytes, _ := json.Marshal(failedResp)
			idempotencyRec.Status = domain.IdempotencyStatusFailed
			idempotencyRec.ResponseCode = 422
			idempotencyRec.ResponseBody = string(respBytes)
			if err := s.repo.Idempotency().Update(ctx, tx, idempotencyRec); err != nil {
				return fmt.Errorf("failed to finalize failed idempotency record: %w", err)
			}

			finalResponse = failedResp
			txErr = domain.ErrInsufficientBalance
			// Return nil to commit the failure audit record into DB
			return nil
		}

		// F. Perform atomic balance updates
		fromWallet.Balance -= req.Amount
		fromWallet.UpdatedAt = now
		if err := s.repo.Wallets().UpdateBalance(ctx, tx, fromWallet); err != nil {
			return fmt.Errorf("failed to debit source wallet: %w", err)
		}

		toWallet.Balance += req.Amount
		toWallet.UpdatedAt = now
		if err := s.repo.Wallets().UpdateBalance(ctx, tx, toWallet); err != nil {
			return fmt.Errorf("failed to credit destination wallet: %w", err)
		}

		// G. Create 2 double-entry ledger entries
		debitEntry := &domain.LedgerEntry{
			ID:           "led_" + uuid.New().String(),
			WalletID:     fromWallet.ID,
			TransferID:   transferID,
			Type:         domain.LedgerEntryTypeDebit,
			Amount:       req.Amount,
			BalanceAfter: fromWallet.Balance,
			CreatedAt:    now,
		}
		creditEntry := &domain.LedgerEntry{
			ID:           "led_" + uuid.New().String(),
			WalletID:     toWallet.ID,
			TransferID:   transferID,
			Type:         domain.LedgerEntryTypeCredit,
			Amount:       req.Amount,
			BalanceAfter: toWallet.Balance,
			CreatedAt:    now,
		}
		if err := s.repo.Ledgers().CreateEntries(ctx, tx, []*domain.LedgerEntry{debitEntry, creditEntry}); err != nil {
			return fmt.Errorf("failed to record ledger entries: %w", err)
		}

		// H. Mark Transfer as PROCESSED
		transfer.Status = domain.TransferStatusProcessed
		transfer.UpdatedAt = now
		if err := s.repo.Transfers().Update(ctx, tx, transfer); err != nil {
			return fmt.Errorf("failed to update transfer status: %w", err)
		}

		// I. Complete Idempotency record with cached response
		successResp := TransferResponse{
			TransferID:     transferID,
			IdempotencyKey: req.IdempotencyKey,
			FromWalletID:   req.FromWalletID,
			ToWalletID:     req.ToWalletID,
			Amount:         req.Amount,
			Currency:       req.Currency,
			Status:         domain.TransferStatusProcessed,
			CreatedAt:      now,
		}
		respBytes, err := json.Marshal(successResp)
		if err != nil {
			return fmt.Errorf("failed to serialize success response: %w", err)
		}

		idempotencyRec.Status = domain.IdempotencyStatusCompleted
		idempotencyRec.ResponseCode = 201
		idempotencyRec.ResponseBody = string(respBytes)
		idempotencyRec.UpdatedAt = now
		if err := s.repo.Idempotency().Update(ctx, tx, idempotencyRec); err != nil {
			return fmt.Errorf("failed to finalize idempotency record: %w", err)
		}

		finalResponse = successResp
		return nil
	})

	if err != nil {
		return nil, err
	}
	if txErr != nil {
		return &finalResponse, txErr
	}

	return &finalResponse, nil
}

func (s *transferService) GetTransfer(ctx context.Context, id string) (*domain.Transfer, error) {
	return s.repo.Transfers().GetByID(ctx, id)
}

func (s *transferService) GetTransferLedger(ctx context.Context, transferID string) ([]*domain.LedgerEntry, error) {
	return s.repo.Ledgers().GetByTransferID(ctx, transferID)
}
