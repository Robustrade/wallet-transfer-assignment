package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"

	"database/sql"
)

type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type TransferService struct {
	db         *sql.DB
	walletRepo *repository.WalletRepository
	txRepo     *repository.TransferRepository
	ledgerRepo *repository.LedgerRepository
}

func NewTransferService(
	db *sql.DB,
	walletRepo *repository.WalletRepository,
	txRepo *repository.TransferRepository,
	ledgerRepo *repository.LedgerRepository,
) *TransferService {
	return &TransferService{
		db:         db,
		walletRepo: walletRepo,
		txRepo:     txRepo,
		ledgerRepo: ledgerRepo,
	}
}

// CreateTransfer executes a wallet-to-wallet transfer atomically.
//
// Returns (transfer, true, nil) when a new transfer was created.
// Returns (transfer, false, nil) when the request is an idempotent replay.
// Returns (nil, false, err) on validation or system errors.
// Returns (transfer, true, domain.ErrInsufficientFunds) when the source wallet
// has insufficient funds; the transfer is persisted with status FAILED.
func (s *TransferService) CreateTransfer(ctx context.Context, req CreateTransferRequest) (*domain.Transfer, bool, error) {
	if req.Amount <= 0 {
		return nil, false, domain.ErrInvalidAmount
	}
	if req.FromWalletID == req.ToWalletID {
		return nil, false, domain.ErrSameWallet
	}

	// Fast idempotency check before acquiring any lock.
	if req.IdempotencyKey != "" {
		existing, err := s.txRepo.GetByIdempotencyKey(ctx, req.IdempotencyKey)
		if err == nil {
			return existing, false, nil
		}
		if !errors.Is(err, domain.ErrTransferNotFound) {
			return nil, false, fmt.Errorf("check idempotency key: %w", err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	fromWallet, err := s.walletRepo.GetByID(ctx, tx, req.FromWalletID)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			return nil, false, fmt.Errorf("source %w", err)
		}
		return nil, false, err
	}

	toWallet, err := s.walletRepo.GetByID(ctx, tx, req.ToWalletID)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			return nil, false, fmt.Errorf("destination %w", err)
		}
		return nil, false, err
	}

	now := time.Now().UTC()
	transfer := &domain.Transfer{
		ID:             uuid.New().String(),
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         domain.StatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.txRepo.Create(ctx, tx, transfer); err != nil {
		// A concurrent request with the same idempotency key beat us to the INSERT.
		// The unique constraint fired — fetch and return the committed record.
		if isUniqueConstraintError(err) && req.IdempotencyKey != "" {
			_ = tx.Rollback()
			existing, fetchErr := s.txRepo.GetByIdempotencyKey(ctx, req.IdempotencyKey)
			if fetchErr != nil {
				return nil, false, fmt.Errorf("fetch existing transfer after race: %w", fetchErr)
			}
			return existing, false, nil
		}
		return nil, false, fmt.Errorf("persist transfer: %w", err)
	}

	// Insufficient funds: record the failure and commit so the caller can
	// observe a FAILED transfer (important for idempotent replays).
	if fromWallet.Balance < req.Amount {
		transfer.Status = domain.StatusFailed
		transfer.UpdatedAt = time.Now().UTC()
		if err := s.txRepo.UpdateStatus(ctx, tx, transfer.ID, domain.StatusFailed, transfer.UpdatedAt); err != nil {
			return nil, false, fmt.Errorf("mark transfer failed: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, false, fmt.Errorf("commit failed transfer: %w", err)
		}
		return transfer, true, domain.ErrInsufficientFunds
	}

	// Update balances.
	now = time.Now().UTC()
	if err := s.walletRepo.UpdateBalance(ctx, tx, fromWallet.ID, fromWallet.Balance-req.Amount, now); err != nil {
		return nil, false, fmt.Errorf("debit source wallet: %w", err)
	}
	if err := s.walletRepo.UpdateBalance(ctx, tx, toWallet.ID, toWallet.Balance+req.Amount, now); err != nil {
		return nil, false, fmt.Errorf("credit destination wallet: %w", err)
	}

	// Double-entry ledger.
	entries := []domain.LedgerEntry{
		{
			ID:         uuid.New().String(),
			WalletID:   fromWallet.ID,
			TransferID: transfer.ID,
			Type:       domain.EntryTypeDebit,
			Amount:     req.Amount,
			CreatedAt:  now,
		},
		{
			ID:         uuid.New().String(),
			WalletID:   toWallet.ID,
			TransferID: transfer.ID,
			Type:       domain.EntryTypeCredit,
			Amount:     req.Amount,
			CreatedAt:  now,
		},
	}
	if err := s.ledgerRepo.CreateEntries(ctx, tx, entries); err != nil {
		return nil, false, fmt.Errorf("create ledger entries: %w", err)
	}

	// Transition to PROCESSED — last step before commit.
	transfer.Status = domain.StatusProcessed
	transfer.UpdatedAt = time.Now().UTC()
	if err := s.txRepo.UpdateStatus(ctx, tx, transfer.ID, domain.StatusProcessed, transfer.UpdatedAt); err != nil {
		return nil, false, fmt.Errorf("mark transfer processed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, false, fmt.Errorf("commit transfer: %w", err)
	}

	return transfer, true, nil
}

func (s *TransferService) GetTransfer(ctx context.Context, id string) (*domain.Transfer, error) {
	t, err := s.txRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func isUniqueConstraintError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
