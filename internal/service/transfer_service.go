package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/wallet-transfer-assignment/internal/domain"
	"github.com/wallet-transfer-assignment/internal/repository"
)

// TransferService orchestrates the transfer workflow with idempotency,
// double-entry ledger, and structured logging.
type TransferService struct {
	txManager repository.TxManager
	logger    *slog.Logger
}

// NewTransferService creates a new TransferService.
func NewTransferService(txManager repository.TxManager, logger *slog.Logger) *TransferService {
	return &TransferService{
		txManager: txManager,
		logger:    logger,
	}
}

// TransferResult holds the HTTP status and response body from a transfer operation.
type TransferResult struct {
	StatusCode int
	Body       []byte
}

// CreateTransfer executes a wallet-to-wallet transfer within a single transaction.
// It handles idempotency, validation, double-entry bookkeeping, and state transitions.
func (s *TransferService) CreateTransfer(ctx context.Context, req *domain.TransferRequest) (*TransferResult, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		s.logger.WarnContext(ctx, "transfer.validation_failed",
			"error", err.Error(),
			"from_wallet", req.FromWalletID,
			"to_wallet", req.ToWalletID,
			"amount", req.Amount,
		)
		return nil, err
	}

	s.logger.InfoContext(ctx, "transfer.initiated",
		"idempotency_key", req.IdempotencyKey,
		"from_wallet", req.FromWalletID,
		"to_wallet", req.ToWalletID,
		"amount", req.Amount,
	)

	var result *TransferResult

	err := s.txManager.WithTransaction(ctx, func(repo repository.Repository) error {
		// Step 1: Check idempotency — if we've seen this key, replay the cached response
		existing, err := repo.GetIdempotencyRecord(ctx, req.IdempotencyKey)
		if err != nil {
			return fmt.Errorf("check idempotency: %w", err)
		}
		if existing != nil {
			s.logger.InfoContext(ctx, "transfer.idempotency_hit",
				"idempotency_key", req.IdempotencyKey,
				"transfer_id", existing.TransferID,
			)
			result = &TransferResult{
				StatusCode: existing.ResponseStatus,
				Body:       []byte(existing.ResponseBody),
			}
			return nil
		}

		// Step 2: Lock wallets (sorted order to prevent deadlocks)
		fromWallet, toWallet, err := repository.LockWalletsInOrder(ctx, repo, req.FromWalletID, req.ToWalletID)
		if err != nil {
			if errors.Is(err, domain.ErrWalletNotFound) {
				return domain.ErrWalletNotFound
			}
			return fmt.Errorf("lock wallets: %w", err)
		}

		_ = toWallet // validated existence; balance is not checked for credit

		// Step 3: Create transfer in PENDING state
		now := time.Now().UTC()
		transferID := uuid.New().String()
		transfer := &domain.Transfer{
			ID:             transferID,
			FromWalletID:   req.FromWalletID,
			ToWalletID:     req.ToWalletID,
			Amount:         req.Amount,
			Status:         domain.TransferStatusPending,
			IdempotencyKey: req.IdempotencyKey,
			CreatedAt:      now,
			UpdatedAt:      now,
		}

		if err := repo.CreateTransfer(ctx, transfer); err != nil {
			return fmt.Errorf("create transfer: %w", err)
		}

		s.logger.InfoContext(ctx, "transfer.created",
			"transfer_id", transferID,
			"status", domain.TransferStatusPending,
		)

		// Step 4: Debit source wallet
		if err := repo.DebitWallet(ctx, req.FromWalletID, req.Amount); err != nil {
			if errors.Is(err, domain.ErrInsufficientBalance) {
				// Mark transfer as FAILED
				if updateErr := repo.UpdateTransferStatus(ctx, transferID, domain.TransferStatusFailed); updateErr != nil {
					return fmt.Errorf("update failed status: %w", updateErr)
				}

				s.logger.WarnContext(ctx, "transfer.failed",
					"transfer_id", transferID,
					"reason", "insufficient balance",
					"from_wallet", req.FromWalletID,
					"balance", fromWallet.Balance,
					"amount", req.Amount,
				)

				// Build failed response and cache it
				resp := &domain.TransferResponse{
					ID:             transferID,
					FromWalletID:   req.FromWalletID,
					ToWalletID:     req.ToWalletID,
					Amount:         req.Amount,
					Status:         domain.TransferStatusFailed,
					IdempotencyKey: req.IdempotencyKey,
				}
				body, _ := json.Marshal(resp)

				if cacheErr := repo.CreateIdempotencyRecord(ctx, &domain.IdempotencyRecord{
					IdempotencyKey: req.IdempotencyKey,
					TransferID:     transferID,
					ResponseStatus: http.StatusUnprocessableEntity,
					ResponseBody:   string(body),
					CreatedAt:      now,
				}); cacheErr != nil {
					return fmt.Errorf("cache failed response: %w", cacheErr)
				}

				result = &TransferResult{
					StatusCode: http.StatusUnprocessableEntity,
					Body:       body,
				}
				return nil
			}
			return fmt.Errorf("debit wallet: %w", err)
		}

		// Step 5: Credit destination wallet
		if err := repo.CreditWallet(ctx, req.ToWalletID, req.Amount); err != nil {
			return fmt.Errorf("credit wallet: %w", err)
		}

		// Step 6: Create double-entry ledger entries
		debitEntry := &domain.LedgerEntry{
			ID:         uuid.New().String(),
			WalletID:   req.FromWalletID,
			TransferID: transferID,
			EntryType:  domain.EntryTypeDebit,
			Amount:     req.Amount,
			CreatedAt:  now,
		}
		creditEntry := &domain.LedgerEntry{
			ID:         uuid.New().String(),
			WalletID:   req.ToWalletID,
			TransferID: transferID,
			EntryType:  domain.EntryTypeCredit,
			Amount:     req.Amount,
			CreatedAt:  now,
		}

		if err := repo.CreateLedgerEntry(ctx, debitEntry); err != nil {
			return fmt.Errorf("create debit ledger entry: %w", err)
		}
		if err := repo.CreateLedgerEntry(ctx, creditEntry); err != nil {
			return fmt.Errorf("create credit ledger entry: %w", err)
		}

		// Step 7: Mark transfer as PROCESSED
		if err := repo.UpdateTransferStatus(ctx, transferID, domain.TransferStatusProcessed); err != nil {
			return fmt.Errorf("update transfer status: %w", err)
		}

		// Step 8: Build success response and cache it
		resp := &domain.TransferResponse{
			ID:             transferID,
			FromWalletID:   req.FromWalletID,
			ToWalletID:     req.ToWalletID,
			Amount:         req.Amount,
			Status:         domain.TransferStatusProcessed,
			IdempotencyKey: req.IdempotencyKey,
		}
		body, _ := json.Marshal(resp)

		if err := repo.CreateIdempotencyRecord(ctx, &domain.IdempotencyRecord{
			IdempotencyKey: req.IdempotencyKey,
			TransferID:     transferID,
			ResponseStatus: http.StatusOK,
			ResponseBody:   string(body),
			CreatedAt:      now,
		}); err != nil {
			return fmt.Errorf("cache success response: %w", err)
		}

		s.logger.InfoContext(ctx, "transfer.completed",
			"transfer_id", transferID,
			"status", domain.TransferStatusProcessed,
		)

		result = &TransferResult{
			StatusCode: http.StatusOK,
			Body:       body,
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}
