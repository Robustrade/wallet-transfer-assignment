// Package service implements the business logic for wallet transfers.
package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/shubh/wallet-service/internal/domain"
)

// TransferService orchestrates the transfer workflow including
// idempotency, concurrency safety, and ledger management.
type TransferService struct {
	txManager    TxManager
	walletRepo   WalletRepository
	transferRepo TransferRepository
	ledgerRepo   LedgerRepository
}

// NewTransferService creates a new TransferService.
func NewTransferService(
	txManager TxManager,
	walletRepo WalletRepository,
	transferRepo TransferRepository,
	ledgerRepo LedgerRepository,
) *TransferService {
	return &TransferService{
		txManager:    txManager,
		walletRepo:   walletRepo,
		transferRepo: transferRepo,
		ledgerRepo:   ledgerRepo,
	}
}

// CreateTransfer executes a wallet-to-wallet transfer with idempotency and
// concurrency guarantees.
//
// The entire operation runs inside a single PostgreSQL transaction:
//  1. Check for existing transfer with the same idempotency key (idempotent replay)
//  2. Lock both wallets with SELECT ... FOR UPDATE (ordered by ID to prevent deadlocks)
//  3. Validate sufficient balance
//  4. Create transfer record
//  5. Create double-entry ledger entries
//  6. Update wallet balances
//  7. Commit
func (s *TransferService) CreateTransfer(ctx context.Context, req domain.TransferRequest) (*domain.Transfer, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var transfer *domain.Transfer

	err := s.txManager.RunInTx(ctx, func(txCtx context.Context) error {
		// Step 1: Idempotency check — return existing transfer if found
		existing, err := s.transferRepo.FindByIdempotencyKey(txCtx, req.IdempotencyKey)
		if err != nil {
			return fmt.Errorf("idempotency check failed: %w", err)
		}
		if existing != nil {
			log.Printf("idempotent replay for key=%s, transfer=%s", req.IdempotencyKey, existing.ID)
			transfer = existing
			
			if existing.Status == domain.TransferStatusFailed && existing.FailureReason != nil && *existing.FailureReason == "insufficient funds" {
				return domain.ErrInsufficientFunds
			}
			return nil
		}

		// Step 2: Lock both wallets in deterministic order
		fromWallet, toWallet, err := s.walletRepo.LockWalletsForTransfer(txCtx, req.FromWalletID, req.ToWalletID)
		if err != nil {
			return fmt.Errorf("failed to lock wallets: %w", err)
		}

		// Step 3: Validate sufficient funds
		if fromWallet.Balance < req.Amount {
			// Insufficient funds — record as FAILED transfer
			failureReason := "insufficient funds"
			failedTransfer := &domain.Transfer{
				IdempotencyKey: req.IdempotencyKey,
				FromWalletID:   req.FromWalletID,
				ToWalletID:     req.ToWalletID,
				Amount:         req.Amount,
				Status:         domain.TransferStatusFailed,
				FailureReason:  &failureReason,
			}
			if createErr := s.transferRepo.Create(txCtx, failedTransfer); createErr != nil {
				if isUniqueViolation(createErr) {
					// We need to signal a concurrent duplicate but since TxManager rolls back on error,
					// we return a special sentinel or just the error and handle it outside?
					// Wait, if it fails here, returning a specific error triggers rollback, then we handle it.
					return errConcurrentDuplicate
				}
				return fmt.Errorf("failed to record failed transfer: %w", createErr)
			}
			transfer = failedTransfer
			return domain.ErrInsufficientFunds
		}

		// Step 4: Create transfer record as PROCESSED
		newTransfer := &domain.Transfer{
			IdempotencyKey: req.IdempotencyKey,
			FromWalletID:   req.FromWalletID,
			ToWalletID:     req.ToWalletID,
			Amount:         req.Amount,
			Status:         domain.TransferStatusProcessed,
		}
		if err = s.transferRepo.Create(txCtx, newTransfer); err != nil {
			// Handle unique constraint violation (concurrent duplicate request)
			if isUniqueViolation(err) {
				return errConcurrentDuplicate
			}
			return fmt.Errorf("failed to create transfer: %w", err)
		}

		// Step 5: Create double-entry ledger entries
		if err = s.ledgerRepo.CreatePair(txCtx, newTransfer.ID, req.FromWalletID, req.ToWalletID, req.Amount); err != nil {
			return fmt.Errorf("failed to create ledger entries: %w", err)
		}

		// Step 6: Update wallet balances
		if err = s.walletRepo.UpdateBalance(txCtx, req.FromWalletID, fromWallet.Balance-req.Amount); err != nil {
			return fmt.Errorf("failed to debit sender: %w", err)
		}
		if err = s.walletRepo.UpdateBalance(txCtx, req.ToWalletID, toWallet.Balance+req.Amount); err != nil {
			return fmt.Errorf("failed to credit receiver: %w", err)
		}

		transfer = newTransfer
		return nil
	})

	if errors.Is(err, errConcurrentDuplicate) {
		return s.handleConcurrentDuplicate(ctx, req.IdempotencyKey)
	}

	if err != nil && !errors.Is(err, domain.ErrInsufficientFunds) {
		return nil, err
	}

	if transfer != nil && transfer.Status == domain.TransferStatusProcessed {
		log.Printf("transfer completed: id=%s, from=%s, to=%s, amount=%d",
			transfer.ID, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount)
	}

	return transfer, err
}

var errConcurrentDuplicate = errors.New("concurrent duplicate idempotency key")

// handleConcurrentDuplicate retrieves the transfer that was created by a
// concurrent request with the same idempotency key.
func (s *TransferService) handleConcurrentDuplicate(ctx context.Context, idempotencyKey string) (*domain.Transfer, error) {
	// Re-read from a fresh transaction or connection
	// Since we are just reading, we don't strictly need a transaction manager here,
	// but we could use one. For simplicity, we just use the repo which will use db directly
	// because GetTxFromContext will fall back to db.
	existing, err := s.transferRepo.FindByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("failed to find concurrent duplicate: %w", err)
	}
	if existing == nil {
		return nil, fmt.Errorf("concurrent duplicate resolved but transfer not found for key=%s", idempotencyKey)
	}

	log.Printf("concurrent duplicate resolved for key=%s, transfer=%s", idempotencyKey, existing.ID)
	
	if existing.Status == domain.TransferStatusFailed && existing.FailureReason != nil && *existing.FailureReason == "insufficient funds" {
		return existing, domain.ErrInsufficientFunds
	}
	
	return existing, nil
}

// GetWallet retrieves a wallet by ID.
func (s *TransferService) GetWallet(ctx context.Context, walletID string) (*domain.Wallet, error) {
	return s.walletRepo.GetByID(ctx, walletID)
}

// isUniqueViolation checks if a PostgreSQL error is a unique constraint violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "23505")
}
