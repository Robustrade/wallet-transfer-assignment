package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"wallet-transfer/internal/database"
	"wallet-transfer/internal/idempotency"
	"wallet-transfer/internal/ledger/domain"
	ledgerrepo "wallet-transfer/internal/ledger/repository"
	transferdomain "wallet-transfer/internal/transfer/domain"
	"wallet-transfer/internal/transfer/dto"
	transferrepo "wallet-transfer/internal/transfer/repository"
	walletdomain "wallet-transfer/internal/wallet/domain"
	walletrepo "wallet-transfer/internal/wallet/repository"
	pkgerrors "wallet-transfer/pkg/errors"
	"wallet-transfer/pkg/logger"

	"github.com/google/uuid"
)

// TransferService handles transfer business logic
type TransferService struct {
	db              *database.Connection
	walletRepo      *walletrepo.WalletRepository
	transferRepo    *transferrepo.TransferRepository
	ledgerRepo      *ledgerrepo.LedgerRepository
	idempotencyRepo *idempotency.IdempotencyRepository
	logger          *logger.Logger
}

// NewTransferService creates a new transfer service
func NewTransferService(
	db *database.Connection,
	walletRepo *walletrepo.WalletRepository,
	transferRepo *transferrepo.TransferRepository,
	ledgerRepo *ledgerrepo.LedgerRepository,
	idempotencyRepo *idempotency.IdempotencyRepository,
	logger *logger.Logger,
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

// CreateTransfer executes a transfer with idempotency guarantee
// This implements exactly-once semantics:
// 1. Check idempotency key - if exists, return cached response
// 2. Begin SERIALIZABLE transaction
// 3. Lock wallets in consistent order (prevents deadlock)
// 4. Verify sufficient funds
// 5. Create transfer and ledger entries atomically
// 6. Record idempotency key
// 7. Commit
func (s *TransferService) CreateTransfer(ctx context.Context, req dto.CreateTransferRequest) (*dto.TransferResponse, error) {
	// Validation
	if req.IdempotencyKey == "" {
		return nil, pkgerrors.ErrMissingIdempotencyKey
	}
	if req.FromWalletID == "" || req.ToWalletID == "" {
		return nil, pkgerrors.ErrInvalidWallet
	}
	if req.FromWalletID == req.ToWalletID {
		return nil, pkgerrors.ErrSelfTransfer
	}
	if req.Amount <= 0 {
		return nil, pkgerrors.ErrInvalidAmount
	}

	// Step 1: Check if idempotency key already exists
	transferID, responseJSON, exists, err := s.idempotencyRepo.GetKey(ctx, req.IdempotencyKey)
	if err != nil {
		s.logger.Error("failed to check idempotency key", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, pkgerrors.ErrInternalError
	}

	if exists {
		s.logger.Info("idempotency key found, returning cached response", map[string]interface{}{
			"idempotencyKey": req.IdempotencyKey,
			"transferId":     transferID,
		})
		// Return cached response
		var response dto.TransferResponse
		if err := json.Unmarshal(responseJSON, &response); err != nil {
			s.logger.Error("failed to unmarshal cached response", map[string]interface{}{
				"error": err.Error(),
			})
			return nil, pkgerrors.ErrInternalError
		}
		return &response, nil
	}

	// Step 2: Begin transaction with SERIALIZABLE isolation
	tx, err := s.db.BeginTx(ctx)
	if err != nil {
		s.logger.Error("failed to begin transaction", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, pkgerrors.ErrTransactionFailed
	}
	defer tx.Rollback()

	// Step 3: Lock wallets in consistent order to prevent deadlock
	fromWallet, toWallet, err := s.lockWalletsInOrder(ctx, tx, req.FromWalletID, req.ToWalletID)
	if err != nil {
		s.logger.Error("failed to lock wallets", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, err
	}

	// Step 4: Verify sufficient funds
	if !fromWallet.CanDebit(req.Amount) {
		s.logger.Warn("insufficient funds", map[string]interface{}{
			"walletId":        fromWallet.ID,
			"balance":         fromWallet.Balance,
			"requestedAmount": req.Amount,
		})
		return nil, pkgerrors.ErrInsufficientFunds
	}

	// Step 5: Create transfer record
	newTransferID := uuid.New().String()
	transfer, err := transferdomain.NewTransfer(newTransferID, req.FromWalletID, req.ToWalletID, req.Amount)
	if err != nil {
		s.logger.Error("failed to create transfer", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, pkgerrors.ErrInternalError
	}

	if err := s.transferRepo.Create(ctx, tx, transfer); err != nil {
		s.logger.Error("failed to insert transfer", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, pkgerrors.ErrDatabaseError
	}

	// Step 6: Update wallet balances
	if err := fromWallet.Debit(req.Amount); err != nil {
		return nil, pkgerrors.ErrInternalError
	}
	if err := toWallet.Credit(req.Amount); err != nil {
		return nil, pkgerrors.ErrInternalError
	}

	if err := s.walletRepo.UpdateBalance(ctx, tx, fromWallet.ID, fromWallet.Balance); err != nil {
		s.logger.Error("failed to update from wallet balance", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, pkgerrors.ErrDatabaseError
	}

	if err := s.walletRepo.UpdateBalance(ctx, tx, toWallet.ID, toWallet.Balance); err != nil {
		s.logger.Error("failed to update to wallet balance", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, pkgerrors.ErrDatabaseError
	}

	// Step 7: Create ledger entries (double-entry bookkeeping)
	debitEntry := domain.NewLedgerEntry(
		uuid.New().String(),
		transfer.ID,
		fromWallet.ID,
		domain.TypeDebit,
		req.Amount,
	)
	if err := s.ledgerRepo.CreateEntry(ctx, tx, debitEntry); err != nil {
		s.logger.Error("failed to create debit ledger entry", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, pkgerrors.ErrDatabaseError
	}

	creditEntry := domain.NewLedgerEntry(
		uuid.New().String(),
		transfer.ID,
		toWallet.ID,
		domain.TypeCredit,
		req.Amount,
	)
	if err := s.ledgerRepo.CreateEntry(ctx, tx, creditEntry); err != nil {
		s.logger.Error("failed to create credit ledger entry", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, pkgerrors.ErrDatabaseError
	}

	// Step 8: Mark transfer as processed
	if err := transfer.MarkProcessed(); err != nil {
		s.logger.Error("failed to mark transfer as processed", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, pkgerrors.ErrInternalError
	}

	if err := s.transferRepo.UpdateStatus(ctx, tx, transfer.ID, transfer.Status, nil); err != nil {
		s.logger.Error("failed to update transfer status", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, pkgerrors.ErrDatabaseError
	}

	// Step 9: Record idempotency key
	response := &dto.TransferResponse{
		TransferID:   transfer.ID,
		FromWalletID: transfer.FromWalletID,
		ToWalletID:   transfer.ToWalletID,
		Amount:       transfer.Amount,
		Status:       string(transfer.Status),
		CreatedAt:    transfer.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if err := s.idempotencyRepo.RecordKey(ctx, tx, req.IdempotencyKey, transfer.ID, response); err != nil {
		if errors.Is(err, idempotency.ErrDuplicateIdempotencyKey) {
			cachedTransferID, cachedResponse, exists, getErr := s.idempotencyRepo.GetKey(ctx, req.IdempotencyKey)
			if getErr != nil {
				s.logger.Error("failed to retrieve cached response after duplicate idempotency key", map[string]interface{}{
					"error": getErr.Error(),
				})
				return nil, pkgerrors.ErrDatabaseError
			}
			if !exists {
				s.logger.Error("duplicate idempotency key found but cached response missing", map[string]interface{}{
					"idempotencyKey": req.IdempotencyKey,
				})
				return nil, pkgerrors.ErrDatabaseError
			}

			var cached dto.TransferResponse
			if err := json.Unmarshal(cachedResponse, &cached); err != nil {
				s.logger.Error("failed to unmarshal cached response after duplicate idempotency key", map[string]interface{}{
					"error": err.Error(),
				})
				return nil, pkgerrors.ErrDatabaseError
			}

			s.logger.Info("idempotency duplicate detected, returning existing response", map[string]interface{}{
				"idempotencyKey": req.IdempotencyKey,
				"transferId":     cachedTransferID,
			})
			return &cached, nil
		}

		s.logger.Error("failed to record idempotency key", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, pkgerrors.ErrDatabaseError
	}

	// Step 10: Commit transaction
	if err := tx.Commit(); err != nil {
		s.logger.Error("failed to commit transaction", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, pkgerrors.ErrTransactionFailed
	}

	s.logger.Info("transfer created successfully", map[string]interface{}{
		"transferId": transfer.ID,
		"amount":     transfer.Amount,
		"fromWallet": transfer.FromWalletID,
		"toWallet":   transfer.ToWalletID,
	})

	return response, nil
}

// lockWalletsInOrder locks wallets in consistent order to prevent deadlock
// Always locks the wallet with smaller ID first
func (s *TransferService) lockWalletsInOrder(ctx context.Context, tx *sql.Tx, walletID1, walletID2 string) (*walletdomain.Wallet, *walletdomain.Wallet, error) {
	var wallet1, wallet2 *walletdomain.Wallet
	var err error

	// Sort to ensure consistent ordering
	if walletID1 < walletID2 {
		wallet1, err = s.walletRepo.GetByIDWithLock(ctx, tx, walletID1)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to lock wallet 1: %w", err)
		}
		if wallet1 == nil {
			return nil, nil, pkgerrors.ErrInvalidWallet
		}

		wallet2, err = s.walletRepo.GetByIDWithLock(ctx, tx, walletID2)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to lock wallet 2: %w", err)
		}
		if wallet2 == nil {
			return nil, nil, pkgerrors.ErrInvalidWallet
		}

		return wallet1, wallet2, nil
	}

	wallet2, err = s.walletRepo.GetByIDWithLock(ctx, tx, walletID2)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lock wallet 2: %w", err)
	}
	if wallet2 == nil {
		return nil, nil, pkgerrors.ErrInvalidWallet
	}

	wallet1, err = s.walletRepo.GetByIDWithLock(ctx, tx, walletID1)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lock wallet 1: %w", err)
	}
	if wallet1 == nil {
		return nil, nil, pkgerrors.ErrInvalidWallet
	}

	return wallet1, wallet2, nil
}

// GetTransfer retrieves a transfer by ID
func (s *TransferService) GetTransfer(ctx context.Context, transferID string) (*dto.GetTransferResponse, error) {
	tx, err := s.db.BeginTxReadOnly(ctx)
	if err != nil {
		return nil, pkgerrors.ErrTransactionFailed
	}
	defer tx.Rollback()

	transfer, err := s.transferRepo.GetByID(ctx, tx, transferID)
	if err != nil {
		s.logger.Error("failed to get transfer", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, pkgerrors.ErrDatabaseError
	}
	if transfer == nil {
		return nil, pkgerrors.ErrTransferNotFound
	}

	response := &dto.GetTransferResponse{
		TransferID:   transfer.ID,
		FromWalletID: transfer.FromWalletID,
		ToWalletID:   transfer.ToWalletID,
		Amount:       transfer.Amount,
		Status:       string(transfer.Status),
		CreatedAt:    transfer.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:    transfer.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if transfer.ErrorReason != nil {
		response.ErrorReason = *transfer.ErrorReason
	}

	return response, nil
}
