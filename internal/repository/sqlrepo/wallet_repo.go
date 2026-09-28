package sqlrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

type walletRepo struct {
	q Querier
	d Dialect
}

const selectWallet = `SELECT id, balance, currency, updated_at FROM wallets WHERE id = ?`

func (r *walletRepo) Get(ctx context.Context, id string) (*domain.Wallet, error) {
	return r.get(ctx, selectWallet, id)
}

// GetForUpdate appends the dialect's lock clause. On Postgres, this row stays
// locked until the transaction ends, so no other transfer can read-then-write
// the same balance concurrently.
func (r *walletRepo) GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	return r.get(ctx, selectWallet+r.d.LockClause(), id)
}

func (r *walletRepo) get(ctx context.Context, query, id string) (*domain.Wallet, error) {
	var w domain.Wallet
	err := r.q.QueryRowContext(ctx, r.d.Rebind(query), id).
		Scan(&w.ID, &w.Balance, &w.Currency, scanTime(&w.UpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("wallet %s: %w", id, domain.ErrWalletNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get wallet %s: %w", id, err)
	}
	return &w, nil
}

// UpdateBalance is a guarded write: "AND balance = ?" makes it a
// compare-and-set on the value the caller read under its row lock. With the
// lock held, the guard always matches. Without it, a concurrent change is
// detected (ErrStaleWrite) instead of being silently overwritten, which a
// plain "SET balance = ?" would do and CHECK (balance >= 0) would not catch.
func (r *walletRepo) UpdateBalance(ctx context.Context, id string, expected, balance int64, updatedAt time.Time) error {
	res, err := r.q.ExecContext(ctx,
		r.d.Rebind(`UPDATE wallets SET balance = ?, updated_at = ? WHERE id = ? AND balance = ?`),
		balance, FormatTime(updatedAt), id, expected)
	if err != nil {
		return fmt.Errorf("update wallet %s balance: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update wallet %s balance: %w", id, err)
	}
	if n == 1 {
		return nil
	}
	// No row matched: either the wallet is gone, or its balance moved.
	// Only this failure path pays for the extra lookup.
	if _, err := r.Get(ctx, id); err != nil {
		return err // domain.ErrWalletNotFound, or a lookup failure
	}
	return fmt.Errorf("wallet %s: balance is no longer %d: %w", id, expected, service.ErrStaleWrite)
}
