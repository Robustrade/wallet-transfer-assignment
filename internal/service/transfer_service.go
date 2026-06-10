package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/prateekgautam/wallet-transfer-assignment/internal/domain"
	"github.com/prateekgautam/wallet-transfer-assignment/internal/repository"
)

// TransferResult wraps the transfer with metadata about whether this was
// a newly created transfer or a cached replay of a previous duplicate request.
type TransferResult struct {
	Transfer    *domain.Transfer
	IsDuplicate bool
}

// TransferService implements the core business logic for wallet transfers.
// It orchestrates idempotency checks, wallet locking, balance updates,
// ledger entries, and transfer state transitions within atomic transactions.
type TransferService struct {
	db              *repository.DB
	walletRepo      WalletRepo
	transferRepo    TransferRepo
	ledgerRepo      LedgerRepo
	idempotencyRepo IdempotencyRepo
	logger          *slog.Logger
}

// NewTransferService creates a new TransferService with all required dependencies.
func NewTransferService(
	db *repository.DB,
	walletRepo WalletRepo,
	transferRepo TransferRepo,
	ledgerRepo LedgerRepo,
	idempotencyRepo IdempotencyRepo,
	logger *slog.Logger,
) *TransferService {
	return &TransferService{
		db:              db,
		walletRepo:      walletRepo,
		transferRepo:    transferRepo,
		ledgerRepo:      ledgerRepo,
		idempotencyRepo: idempotencyRepo,
		logger:          logger,
	}
}

// CreateTransfer executes a wallet-to-wallet transfer with idempotency guarantees.
//
// The operation handles atomic outcomes:
//   - Successful transfers commit: balances updated, ledger entries created,
//     transfer PROCESSED, and idempotency status set to COMPLETED.
//   - Business failures (e.g., wallet-not-found or insufficient funds) commit:
//     transfer status set to FAILED (if applicable) and idempotency status set to FAILED.
//     Subsequent retries with the same key will return the cached failure.
//   - System errors (e.g., database connection loss) roll back the transaction,
//     releasing/freeing the idempotency key for retry.
//
// Concurrency safety is achieved via SELECT FOR UPDATE on wallet rows, with wallets
// locked in deterministic ID order to prevent deadlocks.
//
// Idempotency flow:
//  1. INSERT idempotency record ON CONFLICT DO NOTHING (within the transaction)
//  2. If conflict → the key was already claimed → handle as duplicate (outside tx)
//  3. If inserted → proceed with transfer → mark idempotency COMPLETED or FAILED and commit
//  4. If system error causes rollback → idempotency record also rolls back → key freed for retry
func (s *TransferService) CreateTransfer(ctx context.Context, req domain.CreateTransferRequest) (*TransferResult, error) {
	// 1. Validate request format (no database access needed)
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// 2. Compute request hash for idempotency mismatch detection
	requestHash := domain.ComputeRequestHash(req.FromWalletID, req.ToWalletID, req.Amount)

	// 3. Begin transaction with explicit READ COMMITTED isolation level.
	// READ COMMITTED is sufficient here because the SELECT FOR UPDATE on wallet
	// rows provides the necessary serialization for concurrent transfers.
	tx, err := s.db.Pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted,
	})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback on defer is a safety net

	// Try to acquire transaction-level advisory lock on the idempotency key to prevent concurrent blocking.
	// If the lock is held by another transaction, we fail fast with ErrIdempotencyKeyInProgress (409 Conflict).
	acquired, err := s.idempotencyRepo.TryAdvisoryXactLock(ctx, tx, req.IdempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("check idempotency advisory lock: %w", err)
	}
	if !acquired {
		s.logger.Info("concurrent idempotency key lock request rejected", "key", req.IdempotencyKey)
		return nil, domain.ErrIdempotencyKeyInProgress
	}

	// 4. Claim the idempotency key (INSERT ... ON CONFLICT DO NOTHING)
	//
	// Because of the preceding advisory lock guard, concurrent requests with the same
	// key are rejected early with 409 Conflict. This INSERT is reached only when the lock
	// is successfully acquired, meaning concurrent INSERTs will not block on this unique index.
	created, err := s.idempotencyRepo.Create(ctx, tx, &domain.IdempotencyRecord{
		IdempotencyKey: req.IdempotencyKey,
		RequestHash:    requestHash,
		Status:         domain.IdempotencyStatusInProgress,
	})
	if err != nil {
		return nil, fmt.Errorf("claim idempotency key: %w", err)
	}

	if !created {
		// Key already exists — another transaction committed with this key.
		// Roll back our empty transaction and handle the duplicate outside.
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			s.logger.Warn("rollback after idempotency conflict", "error", rbErr)
		}
		s.logger.Info("duplicate idempotency key detected", "key", req.IdempotencyKey)
		return s.handleDuplicateRequest(ctx, req.IdempotencyKey, requestHash)
	}

	// 5. Lock both wallets with SELECT FOR UPDATE, ordered by ID to prevent deadlocks
	fromWallet, _, err := s.lockWalletsOrdered(ctx, tx, req.FromWalletID, req.ToWalletID)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			// Persist the failure (without transfer ID because of FK constraint) and commit
			if markErr := s.idempotencyRepo.MarkFailed(ctx, tx, req.IdempotencyKey, nil); markErr != nil {
				return nil, fmt.Errorf("mark idempotency status failed for wallet not found: %w", markErr)
			}
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return nil, fmt.Errorf("commit failed transfer: %w", commitErr)
			}
			return nil, domain.ErrWalletNotFound
		}
		// System error -> transaction rolls back (via defer), key freed for retry
		return nil, err
	}

	// 6. Check sender has sufficient funds
	if fromWallet.Balance < req.Amount {
		// Persist the failure and commit
		if _, recordErr := s.recordFailedTransfer(ctx, tx, req, "insufficient funds"); recordErr != nil {
			return nil, recordErr // Will rollback via defer
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, fmt.Errorf("commit failed transfer: %w", commitErr)
		}
		return nil, domain.ErrInsufficientFunds
	}

	// 7. Create transfer in PENDING state
	transfer := &domain.Transfer{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         domain.TransferStatusPending,
	}
	transfer, err = s.transferRepo.Create(ctx, tx, transfer)
	if err != nil {
		return nil, fmt.Errorf("create transfer: %w", err)
	}

	// 8. Debit source wallet
	if err := s.walletRepo.UpdateBalance(ctx, tx, req.FromWalletID, -req.Amount); err != nil {
		return nil, fmt.Errorf("debit source wallet: %w", err)
	}

	// 9. Credit destination wallet
	if err := s.walletRepo.UpdateBalance(ctx, tx, req.ToWalletID, req.Amount); err != nil {
		return nil, fmt.Errorf("credit destination wallet: %w", err)
	}

	// 10. Create double-entry ledger records (DEBIT + CREDIT)
	if err := s.ledgerRepo.CreatePair(ctx, tx, transfer.ID, req.FromWalletID, req.ToWalletID, req.Amount); err != nil {
		return nil, fmt.Errorf("create ledger entries: %w", err)
	}

	// 11. Validate state transition before applying it
	if !transfer.CanTransitionTo(domain.TransferStatusProcessed) {
		return nil, fmt.Errorf("invalid state transition from %s to PROCESSED", transfer.Status)
	}

	// 12. Transition transfer from PENDING → PROCESSED
	if err := s.transferRepo.UpdateStatus(ctx, tx, transfer.ID, domain.TransferStatusProcessed, nil); err != nil {
		return nil, fmt.Errorf("update transfer status: %w", err)
	}
	transfer.Status = domain.TransferStatusProcessed

	// 13. Mark idempotency record as COMPLETED with the transfer ID
	if err := s.idempotencyRepo.MarkCompleted(ctx, tx, req.IdempotencyKey, transfer.ID); err != nil {
		return nil, fmt.Errorf("mark idempotency completed: %w", err)
	}

	// 14. Commit the entire transaction atomically
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	s.logger.Info("transfer completed",
		"transferID", transfer.ID,
		"from", req.FromWalletID,
		"to", req.ToWalletID,
		"amount", req.Amount,
	)

	return &TransferResult{Transfer: transfer, IsDuplicate: false}, nil
}

// handleDuplicateRequest processes a request where the idempotency key was already claimed.
// It verifies the request hash matches the original, then returns the cached transfer.
//
// TOCTOU note: Between our INSERT conflict detection and this read, the other transaction
// may have rolled back (freeing the key). If the record is not found, we return a clear
// retryable error rather than a cryptic failure — the client can safely re-submit.
func (s *TransferService) handleDuplicateRequest(ctx context.Context, key, requestHash string) (*TransferResult, error) {
	record, err := s.idempotencyRepo.GetByKey(ctx, s.db.Pool, key)
	if err != nil {
		return nil, fmt.Errorf("get idempotency record: %w", err)
	}
	if record == nil {
		// The record was rolled back by the other transaction between our conflict
		// detection and this read. This is a narrow TOCTOU window — safe to retry.
		return nil, &domain.DomainError{
			Code:    "RETRYABLE",
			Message: "concurrent request completed; please retry",
		}
	}

	// Verify request parameters match the original request
	if record.RequestHash != requestHash {
		return nil, domain.ErrIdempotencyKeyMismatch
	}

	switch record.Status {
	case domain.IdempotencyStatusCompleted:
		if record.TransferID == nil {
			return nil, fmt.Errorf("corrupted idempotency record: completed without transfer ID")
		}
		s.logger.Info("returning cached transfer for duplicate request", "key", key, "transferID", *record.TransferID)
		transfer, err := s.transferRepo.GetByID(ctx, s.db.Pool, *record.TransferID)
		if err != nil {
			return nil, err
		}
		return &TransferResult{Transfer: transfer, IsDuplicate: true}, nil

	case domain.IdempotencyStatusInProgress:
		// With the advisory lock pre-check, concurrent in-flight requests are rejected early 
		// before reaching this point. This fallback path remains defensively in case the advisory 
		// lock check is bypassed or in rare key hash collision scenarios.
		return nil, domain.ErrIdempotencyKeyInProgress

	case domain.IdempotencyStatusFailed:
		if record.TransferID == nil {
			// If TransferID is nil, it means the failure occurred before the transfer record
			// could be created, which is only possible if one of the wallets was not found.
			return nil, domain.ErrWalletNotFound
		}
		transfer, err := s.transferRepo.GetByID(ctx, s.db.Pool, *record.TransferID)
		if err != nil {
			return nil, err
		}
		reason := ""
		if transfer.ErrorReason != nil {
			reason = *transfer.ErrorReason
		}
		switch reason {
		case "wallet not found":
			return nil, domain.ErrWalletNotFound
		case "insufficient funds":
			return nil, domain.ErrInsufficientFunds
		default:
			return nil, &domain.DomainError{
				Code:    "PREVIOUS_REQUEST_FAILED",
				Message: fmt.Sprintf("previous request failed: %s", reason),
			}
		}

	default:
		return nil, fmt.Errorf("unexpected idempotency status: %s", record.Status)
	}
}

// lockWalletsOrdered acquires row-level locks on two wallets in deterministic order.
// Wallets are locked alphabetically by ID to prevent deadlocks when concurrent
// transfers involve the same pair of wallets (e.g., A→B and B→A).
func (s *TransferService) lockWalletsOrdered(ctx context.Context, tx repository.DBTX, fromID, toID string) (*domain.Wallet, *domain.Wallet, error) {
	firstID, secondID := fromID, toID
	if firstID > secondID {
		firstID, secondID = secondID, firstID
	}

	first, err := s.walletRepo.GetByIDForUpdate(ctx, tx, firstID)
	if err != nil {
		return nil, nil, err
	}

	second, err := s.walletRepo.GetByIDForUpdate(ctx, tx, secondID)
	if err != nil {
		return nil, nil, err
	}

	// Return in caller's expected order (from, to)
	if firstID == fromID {
		return first, second, nil
	}
	return second, first, nil
}

// GetTransfer retrieves a transfer by its ID.
func (s *TransferService) GetTransfer(ctx context.Context, id string) (*domain.Transfer, error) {
	return s.transferRepo.GetByID(ctx, s.db.Pool, id)
}

// CreateWallet creates a new wallet with the given parameters.
func (s *TransferService) CreateWallet(ctx context.Context, req domain.CreateWalletRequest) (*domain.Wallet, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	wallet := &domain.Wallet{
		ID:       req.ID,
		Balance:  req.Balance,
		Currency: req.Currency,
	}

	wallet, err := s.walletRepo.Create(ctx, s.db.Pool, wallet)
	if err != nil {
		return nil, err
	}

	s.logger.Info("wallet created", "walletID", wallet.ID, "balance", wallet.Balance, "currency", wallet.Currency)
	return wallet, nil
}

// GetWallet retrieves a wallet by its ID.
func (s *TransferService) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	return s.walletRepo.GetByID(ctx, s.db.Pool, id)
}

// recordFailedTransfer creates a failed transfer record and marks the idempotency status as FAILED.
func (s *TransferService) recordFailedTransfer(ctx context.Context, tx repository.DBTX, req domain.CreateTransferRequest, reason string) (*domain.Transfer, error) {
	// Create transfer in FAILED state
	transfer := &domain.Transfer{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         domain.TransferStatusFailed,
		ErrorReason:    &reason,
	}
	transfer, err := s.transferRepo.Create(ctx, tx, transfer)
	if err != nil {
		return nil, fmt.Errorf("create failed transfer: %w", err)
	}

	// Mark idempotency record as FAILED with the transfer ID
	if err := s.idempotencyRepo.MarkFailed(ctx, tx, req.IdempotencyKey, &transfer.ID); err != nil {
		return nil, fmt.Errorf("mark idempotency status failed: %w", err)
	}

	return transfer, nil
}
