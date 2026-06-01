package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/timeutil"
	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/tx"
)

// ErrInsufficientBalance is returned when a debit would take a balance below zero.
var ErrInsufficientBalance = errors.New("ledger: insufficient balance")

// ErrWalletNotFound is returned when a wallet balance record does not exist.
var ErrWalletNotFound = errors.New("ledger: wallet not found")

// Service is the sole writer of ledger_entries and wallet_balances.
// It owns the transaction boundary — callers never pass or manage a tx.
type Service struct {
	ledgerRepo  LedgerRepository
	balanceRepo BalanceRepository
	db          tx.Starter // platform-level transaction contract
}

// NewService constructs a LedgerService.
func NewService(ledgerRepo LedgerRepository, balanceRepo BalanceRepository, db tx.Starter) *Service {
	return &Service{ledgerRepo: ledgerRepo, balanceRepo: balanceRepo, db: db}
}

// GetBalance returns the current balance for a wallet.
func (s *Service) GetBalance(ctx context.Context, walletID string) (*Balance, error) {
	b, err := s.balanceRepo.GetBalance(ctx, walletID)
	if err != nil {
		return nil, fmt.Errorf("ledger.Service.GetBalance: %w", err)
	}
	return b, nil
}

// CommitToLedger is the single atomic entry point for recording a transfer in the ledger.
// It owns the full transaction — BeginTx, Commit, and Rollback are all internal.
// Callers do not pass or manage any transaction object.
//
// Atomically:
//  1. BEGIN transaction.
//  2. SELECT FOR UPDATE on both balance rows (prevents concurrent double-spend).
//  3. Validates source wallet has sufficient balance.
//  4. Applies debit to fromWalletID and credit to toWalletID (balanceRepo).
//  5. Inserts exactly one DEBIT and one CREDIT ledger entry (ledgerRepo).
//  6. COMMIT. On any error, ROLLBACK before returning.
//
// Returns ErrInsufficientBalance if the source balance would go negative.
func (s *Service) CommitToLedger(ctx context.Context, transferID, fromWalletID, toWalletID string, amount int64) error {
	txCtx, tx, err := s.db.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("ledger.CommitToLedger: begin tx: %w", err)
	}

	// balanceRepo: row lock + balance check + debit/credit update.
	if err := s.balanceRepo.DebitAndCredit(txCtx, fromWalletID, toWalletID, amount); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("ledger.CommitToLedger: balance: %w", err)
	}

	// ledgerRepo: double-entry insert.
	// Composite key (transfer_id, type) is UNIQUE in the DB — a duplicate
	// CommitToLedger call for the same transferID is caught before any row is written.
	now := timeutil.Now()
	debit := &Entry{
		TransferID: transferID,
		WalletID:   fromWalletID,
		Type:       EntryTypeDebit,
		Amount:     amount,
		CreatedAt:  now,
	}
	credit := &Entry{
		TransferID: transferID,
		WalletID:   toWalletID,
		Type:       EntryTypeCredit,
		Amount:     amount,
		CreatedAt:  now,
	}

	if err := s.ledgerRepo.InsertDoubleEntry(txCtx, debit, credit); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("ledger.CommitToLedger: entries: %w", err)
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("ledger.CommitToLedger: commit: %w", err)
	}

	return nil
}
