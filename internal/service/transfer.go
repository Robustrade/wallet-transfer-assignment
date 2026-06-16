package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/synchone/wallet-transfer-assignment/internal/domain"
	"github.com/synchone/wallet-transfer-assignment/internal/repository"
)

type TransferRepository interface {
	BeginTx(ctx context.Context) (pgx.Tx, error)
	FindTransferForUpdate(ctx context.Context, tx pgx.Tx, key string) (*domain.Transfer, error)
	CreateTransfer(ctx context.Context, tx pgx.Tx, input domain.CreateTransferInput) (*domain.Transfer, error)
	LockWallets(ctx context.Context, tx pgx.Tx, walletIDs ...string) (map[string]domain.Wallet, error)
	UpdateWalletBalance(ctx context.Context, tx pgx.Tx, walletID string, delta int64) error
	InsertLedgerEntry(ctx context.Context, tx pgx.Tx, transferID uuid.UUID, walletID string, entryType domain.LedgerEntryType, amount int64) (int64, error)
	ListLedgerEntries(ctx context.Context, tx pgx.Tx, transferID uuid.UUID) ([]domain.LedgerEntry, error)
	UpdateTransferStatus(ctx context.Context, tx pgx.Tx, transferID uuid.UUID, status domain.TransferStatus, failureReason *string) error
	StoreResponseSnapshot(ctx context.Context, tx pgx.Tx, transferID uuid.UUID, snapshot json.RawMessage) error
}

type TransferService struct {
	repo   TransferRepository
	logger *slog.Logger
}

func NewTransferService(repo TransferRepository, logger *slog.Logger) *TransferService {
	return &TransferService{repo: repo, logger: logger}
}

func (s *TransferService) CreateTransfer(ctx context.Context, input domain.CreateTransferInput) (domain.TransferResult, error) {
	if err := validateInput(input); err != nil {
		return domain.TransferResult{}, err
	}

	var lastErr error
	for attempt := range 3 {
		result, err := s.createTransferOnce(ctx, input)
		if err == nil {
			return result, nil
		}
		if !isRetryable(err) {
			return domain.TransferResult{}, err
		}
		lastErr = err
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
	return domain.TransferResult{}, lastErr
}

func (s *TransferService) createTransferOnce(ctx context.Context, input domain.CreateTransferInput) (domain.TransferResult, error) {
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return domain.TransferResult{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	transfer, isReplay, wallets, err := s.loadOrCreateTransfer(ctx, tx, input)
	if err != nil {
		return domain.TransferResult{}, err
	}

	if len(transfer.ResponseSnapshot) > 0 {
		result, err := domain.TransferResultFromSnapshot(transfer.ResponseSnapshot)
		if err != nil {
			return domain.TransferResult{}, fmt.Errorf("decode snapshot: %w", err)
		}
		s.logger.Info("transfer.duplicate", "transfer_id", result.TransferID, "idempotency_key", input.IdempotencyKey)
		if err := tx.Commit(ctx); err != nil {
			return domain.TransferResult{}, fmt.Errorf("commit: %w", err)
		}
		return result, nil
	}

	if transfer.Status == domain.StatusProcessed || transfer.Status == domain.StatusFailed {
		result, err := s.buildResultFromTransfer(ctx, tx, transfer)
		if err != nil {
			return domain.TransferResult{}, err
		}
		if isReplay {
			result.IsReplay = true
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.TransferResult{}, fmt.Errorf("commit: %w", err)
		}
		return result, nil
	}

	result, err := s.processTransfer(ctx, tx, transfer, wallets)
	if err != nil {
		return domain.TransferResult{}, err
	}
	if isReplay {
		result.IsReplay = true
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.TransferResult{}, fmt.Errorf("commit: %w", err)
	}

	switch result.Status {
	case domain.StatusProcessed:
		s.logger.Info("transfer.processed", "transfer_id", result.TransferID, "idempotency_key", input.IdempotencyKey)
	case domain.StatusFailed:
		s.logger.Info("transfer.failed", "transfer_id", result.TransferID, "idempotency_key", input.IdempotencyKey, "reason", result.FailureReason)
	}

	return result, nil
}

func (s *TransferService) loadOrCreateTransfer(ctx context.Context, tx pgx.Tx, input domain.CreateTransferInput) (*domain.Transfer, bool, map[string]domain.Wallet, error) {
	existing, err := s.repo.FindTransferForUpdate(ctx, tx, input.IdempotencyKey)
	if err == nil {
		return existing, true, nil, nil
	}
	if !errors.Is(err, domain.ErrTransferNotFound) {
		return nil, false, nil, fmt.Errorf("find transfer: %w", err)
	}

	wallets, err := s.repo.LockWallets(ctx, tx, input.FromWalletID, input.ToWalletID)
	if err != nil {
		return nil, false, nil, fmt.Errorf("lock wallets: %w", err)
	}
	if _, ok := wallets[input.FromWalletID]; !ok {
		return nil, false, nil, domain.ErrWalletNotFound
	}
	if _, ok := wallets[input.ToWalletID]; !ok {
		return nil, false, nil, domain.ErrWalletNotFound
	}

	transfer, err := s.repo.CreateTransfer(ctx, tx, input)
	if err != nil {
		if repository.IsUniqueViolation(err) {
			transfer, isReplay, loadErr := s.loadExistingAfterConflict(ctx, tx, input.IdempotencyKey)
			return transfer, isReplay, nil, loadErr
		}
		return nil, false, nil, fmt.Errorf("create transfer: %w", err)
	}
	return transfer, false, wallets, nil
}

func (s *TransferService) loadExistingAfterConflict(ctx context.Context, tx pgx.Tx, key string) (*domain.Transfer, bool, error) {
	transfer, err := s.repo.FindTransferForUpdate(ctx, tx, key)
	if err != nil {
		return nil, false, fmt.Errorf("reload transfer: %w", err)
	}
	return transfer, true, nil
}

func (s *TransferService) processTransfer(ctx context.Context, tx pgx.Tx, transfer *domain.Transfer, lockedWallets map[string]domain.Wallet) (domain.TransferResult, error) {
	if transfer.Status == domain.StatusFailed {
		return s.buildResultFromTransfer(ctx, tx, transfer)
	}

	existingEntries, err := s.repo.ListLedgerEntries(ctx, tx, transfer.ID)
	if err != nil {
		return domain.TransferResult{}, fmt.Errorf("list ledger entries: %w", err)
	}
	if len(existingEntries) == 2 {
		return s.finalizeExistingTransfer(ctx, tx, transfer, existingEntries)
	}
	if len(existingEntries) > 0 {
		return domain.TransferResult{}, fmt.Errorf("unexpected partial ledger for transfer %s", transfer.ID)
	}

	wallets := lockedWallets
	if wallets == nil {
		var err error
		wallets, err = s.repo.LockWallets(ctx, tx, transfer.FromWalletID, transfer.ToWalletID)
		if err != nil {
			return domain.TransferResult{}, fmt.Errorf("lock wallets: %w", err)
		}
	}

	fromWallet, ok := wallets[transfer.FromWalletID]
	if !ok {
		return s.failTransfer(ctx, tx, transfer, domain.ErrWalletNotFound.Error())
	}
	if _, ok := wallets[transfer.ToWalletID]; !ok {
		return s.failTransfer(ctx, tx, transfer, domain.ErrWalletNotFound.Error())
	}

	if fromWallet.Balance < transfer.Amount {
		return s.failTransfer(ctx, tx, transfer, domain.ErrInsufficientFunds.Error())
	}

	if err := s.repo.UpdateWalletBalance(ctx, tx, transfer.FromWalletID, -transfer.Amount); err != nil {
		return domain.TransferResult{}, fmt.Errorf("debit wallet: %w", err)
	}
	if err := s.repo.UpdateWalletBalance(ctx, tx, transfer.ToWalletID, transfer.Amount); err != nil {
		return domain.TransferResult{}, fmt.Errorf("credit wallet: %w", err)
	}

	debitID, err := s.repo.InsertLedgerEntry(ctx, tx, transfer.ID, transfer.FromWalletID, domain.EntryDebit, transfer.Amount)
	if err != nil {
		return domain.TransferResult{}, fmt.Errorf("insert debit: %w", err)
	}
	creditID, err := s.repo.InsertLedgerEntry(ctx, tx, transfer.ID, transfer.ToWalletID, domain.EntryCredit, transfer.Amount)
	if err != nil {
		return domain.TransferResult{}, fmt.Errorf("insert credit: %w", err)
	}

	if err := s.repo.UpdateTransferStatus(ctx, tx, transfer.ID, domain.StatusProcessed, nil); err != nil {
		return domain.TransferResult{}, fmt.Errorf("update status: %w", err)
	}

	result := domain.TransferResult{
		TransferID:     transfer.ID.String(),
		Status:         domain.StatusProcessed,
		FromWalletID:   transfer.FromWalletID,
		ToWalletID:     transfer.ToWalletID,
		Amount:         transfer.Amount,
		LedgerEntryIDs: []int64{debitID, creditID},
	}

	return s.persistSnapshot(ctx, tx, transfer.ID, result)
}

func (s *TransferService) finalizeExistingTransfer(ctx context.Context, tx pgx.Tx, transfer *domain.Transfer, entries []domain.LedgerEntry) (domain.TransferResult, error) {
	if transfer.Status != domain.StatusProcessed {
		if err := s.repo.UpdateTransferStatus(ctx, tx, transfer.ID, domain.StatusProcessed, nil); err != nil {
			return domain.TransferResult{}, fmt.Errorf("update status: %w", err)
		}
	}

	ids := make([]int64, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}

	result := domain.TransferResult{
		TransferID:     transfer.ID.String(),
		Status:         domain.StatusProcessed,
		FromWalletID:   transfer.FromWalletID,
		ToWalletID:     transfer.ToWalletID,
		Amount:         transfer.Amount,
		LedgerEntryIDs: ids,
	}
	return s.persistSnapshot(ctx, tx, transfer.ID, result)
}

func (s *TransferService) failTransfer(ctx context.Context, tx pgx.Tx, transfer *domain.Transfer, reason string) (domain.TransferResult, error) {
	reasonCopy := reason
	if err := s.repo.UpdateTransferStatus(ctx, tx, transfer.ID, domain.StatusFailed, &reasonCopy); err != nil {
		return domain.TransferResult{}, fmt.Errorf("update failed status: %w", err)
	}

	result := domain.TransferResult{
		TransferID:    transfer.ID.String(),
		Status:        domain.StatusFailed,
		FromWalletID:  transfer.FromWalletID,
		ToWalletID:    transfer.ToWalletID,
		Amount:        transfer.Amount,
		FailureReason: reason,
	}
	return s.persistSnapshot(ctx, tx, transfer.ID, result)
}

func (s *TransferService) persistSnapshot(ctx context.Context, tx pgx.Tx, transferID uuid.UUID, result domain.TransferResult) (domain.TransferResult, error) {
	snapshot, err := result.Snapshot()
	if err != nil {
		return domain.TransferResult{}, fmt.Errorf("encode snapshot: %w", err)
	}
	if err := s.repo.StoreResponseSnapshot(ctx, tx, transferID, snapshot); err != nil {
		return domain.TransferResult{}, fmt.Errorf("store snapshot: %w", err)
	}
	return result, nil
}

func (s *TransferService) buildResultFromTransfer(ctx context.Context, tx pgx.Tx, transfer *domain.Transfer) (domain.TransferResult, error) {
	result := domain.TransferResult{
		TransferID:   transfer.ID.String(),
		Status:       transfer.Status,
		FromWalletID: transfer.FromWalletID,
		ToWalletID:   transfer.ToWalletID,
		Amount:       transfer.Amount,
	}
	if transfer.FailureReason != nil {
		result.FailureReason = *transfer.FailureReason
	}

	if transfer.Status == domain.StatusProcessed {
		entries, err := s.repo.ListLedgerEntries(ctx, tx, transfer.ID)
		if err != nil {
			return domain.TransferResult{}, err
		}
		for _, e := range entries {
			result.LedgerEntryIDs = append(result.LedgerEntryIDs, e.ID)
		}
	}
	return result, nil
}

func validateInput(input domain.CreateTransferInput) error {
	if input.IdempotencyKey == "" {
		return domain.ErrMissingIdempotency
	}
	if input.Amount <= 0 {
		return domain.ErrInvalidAmount
	}
	if input.FromWalletID == input.ToWalletID {
		return domain.ErrSameWallet
	}
	if input.FromWalletID == "" || input.ToWalletID == "" {
		return domain.ErrInvalidRequest
	}
	return nil
}

func isRetryable(err error) bool {
	return strings.Contains(err.Error(), "deadlock detected")
}

var _ TransferRepository = (*repository.Repository)(nil)
