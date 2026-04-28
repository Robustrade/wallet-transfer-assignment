package service

import (
	"context"
	"fmt"

	"wallet-transfer/internal/transfer"
	transferRepo "wallet-transfer/internal/transfer/repository"
	"wallet-transfer/internal/transfer/static"
	"wallet-transfer/internal/wallet"
	walletRepo "wallet-transfer/internal/wallet/repository"
	"wallet-transfer/pkg/apperror"
	"wallet-transfer/pkg/database"
	"wallet-transfer/pkg/helper"
	"wallet-transfer/pkg/logger"
	"wallet-transfer/pkg/validate"
)

// TransferService defines business operations for transfers.
type TransferService interface {
	CreateTransfer(ctx context.Context, req *transfer.CreateTransferRequest) (*transfer.TransferResponse, error)
	GetTransfer(ctx context.Context, id string) (*transfer.TransferResponse, error)
	GetAllTransfers(ctx context.Context) ([]*transfer.Transfer, error)
}

type transferService struct {
	transferRepo transferRepo.TransferRepository
	walletRepo   walletRepo.WalletRepository
	pool         database.DBTX
}

// NewTransferService creates a new TransferService.
func NewTransferService(
	pool database.DBTX,
	tr transferRepo.TransferRepository,
	wr walletRepo.WalletRepository,
) TransferService {
	return &transferService{
		transferRepo: tr,
		walletRepo:   wr,
		pool:         pool,
	}
}

// CreateTransfer executes a wallet-to-wallet transfer atomically.
//
// Idempotency: if an idempotencyKey is provided and a transfer with that key
// already exists, the original result is returned without re-executing.
//
// Concurrency: wallets are locked with SELECT … FOR UPDATE in ascending ID
// order inside a single database transaction, preventing deadlocks and
// ensuring no double-spending under concurrent requests.
func (s *transferService) CreateTransfer(ctx context.Context, req *transfer.CreateTransferRequest) (*transfer.TransferResponse, error) {
	const fn = "transferService.CreateTransfer"

	// ── 1. Validate request ──────────────────────────────────────────────────
	if errStr := validate.Validate(req); errStr != "" {
		return nil, apperror.BadRequest(errStr)
	}
	if req.FromWalletID == req.ToWalletID {
		return nil, apperror.BadRequest(static.SameWalletError)
	}

	// ── 2. Idempotency check ─────────────────────────────────────────────────
	if req.IdempotencyKey != "" {
		existing, err := s.transferRepo.GetTransferByIdempotencyKey(ctx, s.pool, req.IdempotencyKey)
		if err != nil {
			logger.Error(fn, "idempotency lookup failed", err, map[string]interface{}{"key": req.IdempotencyKey})
			return nil, apperror.InternalServerError("idempotency check failed")
		}
		if existing != nil {
			if existing.FromWalletID != req.FromWalletID || existing.ToWalletID != req.ToWalletID || existing.Amount != req.Amount {
				return nil, apperror.Conflict("idempotency key reused with different request parameters")
			}
			logger.Info(fn, "duplicate request — returning original result", map[string]interface{}{
				"idempotency_key": req.IdempotencyKey,
				"transfer_id":     existing.ID,
				"status":          existing.Status,
			})
			return s.buildResponse(ctx, existing)
		}
	}

	// ── 3. Begin transaction ─────────────────────────────────────────────────
	tx, err := database.BeginTx(ctx)
	if err != nil {
		logger.Error(fn, "begin tx failed", err, nil)
		return nil, apperror.InternalServerError("failed to begin transaction")
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// ── 4. Create transfer in PENDING state ──────────────────────────────────
	newTransfer := &transfer.Transfer{
		ID:             helper.GenerateID("TXN"),
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         static.StatusPending,
		AuditLog: wallet.AuditLog{
			CreatedBy: "system",
			UpdatedBy: "system",
		},
	}

	if err := s.transferRepo.CreateTransfer(ctx, tx, newTransfer); err != nil {
		if database.IsUniqueViolation(err) && req.IdempotencyKey != "" {
			tx.Rollback(ctx) //nolint:errcheck
			existing, fErr := s.transferRepo.GetTransferByIdempotencyKey(ctx, s.pool, req.IdempotencyKey)
			if fErr == nil && existing != nil {
				logger.Info(fn, "race resolved — returning concurrent result", map[string]interface{}{
					"idempotency_key": req.IdempotencyKey,
				})
				return s.buildResponse(ctx, existing)
			}
		}
		logger.Error(fn, "create transfer record failed", err, nil)
		return nil, apperror.InternalServerError("failed to create transfer")
	}

	// ── 5. Lock both wallets in ascending ID order to prevent deadlocks ──────
	firstID, secondID := req.FromWalletID, req.ToWalletID
	if firstID > secondID {
		firstID, secondID = secondID, firstID
	}

	firstWallet, err := s.walletRepo.GetByIDForUpdate(ctx, tx, firstID)
	if err != nil || firstWallet == nil {
		s.markFailed(ctx, tx, newTransfer.ID, fmt.Sprintf("wallet not found: %s", firstID))
		tx.Commit(ctx) //nolint:errcheck
		return nil, apperror.NotFound(static.WalletNotFound)
	}

	secondWallet, err := s.walletRepo.GetByIDForUpdate(ctx, tx, secondID)
	if err != nil || secondWallet == nil {
		s.markFailed(ctx, tx, newTransfer.ID, fmt.Sprintf("wallet not found: %s", secondID))
		tx.Commit(ctx) //nolint:errcheck
		return nil, apperror.NotFound(static.WalletNotFound)
	}

	// Map first/second back to from/to.
	fromWallet, toWallet := firstWallet, secondWallet
	if firstID == req.ToWalletID {
		fromWallet, toWallet = secondWallet, firstWallet
	}

	// ── 6. Balance check ─────────────────────────────────────────────────────
	if fromWallet.Balance < req.Amount {
		s.markFailed(ctx, tx, newTransfer.ID, static.InsufficientBalance)
		tx.Commit(ctx) //nolint:errcheck
		logger.Warn(fn, "insufficient balance", map[string]interface{}{
			"wallet_id": fromWallet.ID,
			"balance":   fromWallet.Balance,
			"requested": req.Amount,
		})
		return nil, apperror.BadRequest(static.InsufficientBalance)
	}

	// ── 7. Update wallet balances ────────────────────────────────────────────
	if err := s.walletRepo.UpdateBalance(ctx, tx, req.FromWalletID, fromWallet.Balance-req.Amount, "system"); err != nil {
		logger.Error(fn, "debit wallet failed", err, map[string]interface{}{"wallet_id": req.FromWalletID})
		return nil, apperror.InternalServerError("failed to debit wallet")
	}
	if err := s.walletRepo.UpdateBalance(ctx, tx, req.ToWalletID, toWallet.Balance+req.Amount, "system"); err != nil {
		logger.Error(fn, "credit wallet failed", err, map[string]interface{}{"wallet_id": req.ToWalletID})
		return nil, apperror.InternalServerError("failed to credit wallet")
	}

	// ── 8. Create double-entry ledger entries ─────────────────────────────────
	debit := &transfer.LedgerEntry{
		ID:         helper.GenerateID("LED"),
		WalletID:   req.FromWalletID,
		TransferID: newTransfer.ID,
		Type:       static.TypeDebit,
		Amount:     req.Amount,
		CreatedBy:  "system",
	}
	credit := &transfer.LedgerEntry{
		ID:         helper.GenerateID("LED"),
		WalletID:   req.ToWalletID,
		TransferID: newTransfer.ID,
		Type:       static.TypeCredit,
		Amount:     req.Amount,
		CreatedBy:  "system",
	}

	if err := s.transferRepo.CreateLedgerEntry(ctx, tx, debit); err != nil {
		logger.Error(fn, "create debit entry failed", err, nil)
		return nil, apperror.InternalServerError("failed to create ledger entry")
	}
	if err := s.transferRepo.CreateLedgerEntry(ctx, tx, credit); err != nil {
		logger.Error(fn, "create credit entry failed", err, nil)
		return nil, apperror.InternalServerError("failed to create ledger entry")
	}

	// ── 9. Mark transfer as PROCESSED ────────────────────────────────────────
	if err := s.transferRepo.UpdateTransferStatus(ctx, tx, newTransfer.ID, static.StatusProcessed, "", "system"); err != nil {
		logger.Error(fn, "update transfer status failed", err, nil)
		return nil, apperror.InternalServerError("failed to finalise transfer")
	}
	newTransfer.Status = static.StatusProcessed

	// ── 10. Commit ───────────────────────────────────────────────────────────
	if err := tx.Commit(ctx); err != nil {
		logger.Error(fn, "commit failed", err, map[string]interface{}{"transfer_id": newTransfer.ID})
		return nil, apperror.InternalServerError("failed to commit transaction")
	}

	logger.Info(fn, "transfer processed successfully", map[string]interface{}{
		"transfer_id":    newTransfer.ID,
		"from_wallet_id": req.FromWalletID,
		"to_wallet_id":   req.ToWalletID,
		"amount":         req.Amount,
	})

	return &transfer.TransferResponse{
		Transfer: newTransfer,
		Ledger:   []*transfer.LedgerEntry{debit, credit},
	}, nil
}

func (s *transferService) GetTransfer(ctx context.Context, id string) (*transfer.TransferResponse, error) {
	const fn = "transferService.GetTransfer"

	t, err := s.transferRepo.GetTransferByID(ctx, s.pool, id)
	if err != nil {
		logger.Error(fn, "query failed", err, map[string]interface{}{"transfer_id": id})
		return nil, apperror.InternalServerError("failed to get transfer")
	}
	if t == nil {
		return nil, apperror.NotFound(static.TransferNotFound)
	}
	return s.buildResponse(ctx, t)
}

func (s *transferService) GetAllTransfers(ctx context.Context) ([]*transfer.Transfer, error) {
	const fn = "transferService.GetAllTransfers"

	transfers, err := s.transferRepo.GetAllTransfers(ctx, s.pool)
	if err != nil {
		logger.Error(fn, "query all failed", err, nil)
		return nil, apperror.InternalServerError("failed to get transfers")
	}
	return transfers, nil
}

// buildResponse fetches ledger entries and assembles a TransferResponse.
func (s *transferService) buildResponse(ctx context.Context, t *transfer.Transfer) (*transfer.TransferResponse, error) {
	entries, err := s.transferRepo.GetLedgerEntriesByTransferID(ctx, s.pool, t.ID)
	if err != nil {
		return nil, apperror.InternalServerError("failed to get ledger entries")
	}
	return &transfer.TransferResponse{Transfer: t, Ledger: entries}, nil
}

// markFailed updates a transfer to FAILED status within the current transaction.
func (s *transferService) markFailed(ctx context.Context, db database.DBTX, id, reason string) {
	const fn = "transferService.markFailed"
	if err := s.transferRepo.UpdateTransferStatus(ctx, db, id, static.StatusFailed, reason, "system"); err != nil {
		logger.Error(fn, "failed to mark transfer as failed", err, map[string]interface{}{"transfer_id": id})
	}
}
