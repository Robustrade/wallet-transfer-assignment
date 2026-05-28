package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/domain"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/repository"
)

// WalletRepo is the PostgreSQL implementation of
// repository.WalletRepository. It is stateless; the *sql.DB or *sql.Tx
// is passed in per call via Executor.
type WalletRepo struct{}

// NewWalletRepo returns a stateless WalletRepo. There's no state to
// hold but the constructor is kept for symmetry with the other repos
// (it also means a future change to add fields would not break callers).
func NewWalletRepo() *WalletRepo { return &WalletRepo{} }

// Get reads a wallet by id. Used by callers that do not intend to
// mutate the wallet — for example the GET /wallets/{id} balance
// endpoint and post-lock balance reads.
//
// Returns repository.ErrNotFound if no row matches.
func (r *WalletRepo) Get(ctx context.Context, exec repository.Executor, id string) (*domain.Wallet, error) {
	const q = `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1`
	return scanWallet(exec.QueryRowContext(ctx, q, id))
}

// LockForUpdate acquires a row-level write lock on the wallet for the
// lifetime of the surrounding transaction.
//
// Concurrent callers requesting FOR UPDATE on the same row will block
// until the holder commits or rolls back. This is the primary
// concurrency primitive that prevents double-spending: the service
// always locks before reading the balance, so two debits against the
// same wallet serialise on this row's lock instead of racing.
//
// Callers MUST always lock multiple wallets in a deterministic order
// (lex ascending by id, via service.orderedPair) to avoid deadlocks.
func (r *WalletRepo) LockForUpdate(ctx context.Context, exec repository.Executor, id string) (*domain.Wallet, error) {
	const q = `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1 FOR UPDATE`
	return scanWallet(exec.QueryRowContext(ctx, q, id))
}

// UpdateBalance writes a new absolute balance. The service is expected
// to have already taken a FOR UPDATE lock and computed the new value
// from the read-and-validated old one; this method does not perform
// any arithmetic of its own.
//
// Returns repository.ErrNotFound if the row vanished between read and
// write (which under FOR UPDATE should not happen, but we surface it
// rather than silently passing).
func (r *WalletRepo) UpdateBalance(ctx context.Context, exec repository.Executor, id string, balance int64) error {
	const q = `UPDATE wallets SET balance = $2, updated_at = now() WHERE id = $1`
	res, err := exec.ExecContext(ctx, q, id, balance)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// Insert creates a wallet row. Uses RETURNING to fetch the
// database-assigned timestamps so callers get a fully populated entity
// without a follow-up SELECT.
//
// Duplicate ids surface as a unique violation from Postgres
// (SQLSTATE 23505); the caller is expected to handle that — see the
// POST /wallets handler and the service.WalletService.Create wrapper.
func (r *WalletRepo) Insert(ctx context.Context, exec repository.Executor, w *domain.Wallet) error {
	const q = `
		INSERT INTO wallets (id, balance)
		VALUES ($1, $2)
		RETURNING created_at, updated_at`
	row := exec.QueryRowContext(ctx, q, w.ID, w.Balance)
	if err := row.Scan(&w.CreatedAt, &w.UpdatedAt); err != nil {
		return fmt.Errorf("insert wallet: %w", err)
	}
	return nil
}

// scanWallet centralises the scan + sql.ErrNoRows -> ErrNotFound
// translation so every read path returns the same sentinel.
func scanWallet(row *sql.Row) (*domain.Wallet, error) {
	var w domain.Wallet
	if err := row.Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("scan wallet: %w", err)
	}
	return &w, nil
}
