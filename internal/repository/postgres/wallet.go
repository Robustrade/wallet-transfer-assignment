package postgres

import (
	"context"
	"fmt"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

func (t *transaction) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	var w domain.Wallet
	if t.failure != nil {
		return w, t.failure
	}
	err := t.tx.QueryRowContext(ctx, `SELECT id, balance, created_at, updated_at FROM wallets WHERE id=$1`, id).Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	return w, t.record("get wallet", err)
}
func (t *transaction) LockWallets(ctx context.Context, fromID, toID string) (from, to domain.Wallet, err error) {
	if t.failure != nil {
		return from, to, t.failure
	}
	if fromID == toID {
		return from, to, fmt.Errorf("lock distinct wallets: %w", repository.ErrConflict)
	}
	// Keep ORDER BY and FOR UPDATE at the same query level. Wallet IDs are
	// immutable in this service; both transfer directions therefore acquire rows
	// in the same database order. Map business roles by ID below, not row position.
	rows, err := t.tx.QueryContext(ctx, `SELECT id, balance, created_at, updated_at FROM wallets WHERE id IN ($1, $2) ORDER BY id FOR UPDATE`, fromID, toID)
	if err != nil {
		return from, to, t.record("lock wallets", err)
	}
	defer rows.Close()
	foundFrom, foundTo := false, false
	for rows.Next() {
		var w domain.Wallet
		if err := rows.Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return from, to, t.record("scan locked wallet", err)
		}
		if w.ID == fromID {
			from, foundFrom = w, true
		}
		if w.ID == toID {
			to, foundTo = w, true
		}
	}
	if err := rows.Err(); err != nil {
		return from, to, t.record("read locked wallets", err)
	}
	if !foundFrom || !foundTo {
		return from, to, fmt.Errorf("lock wallets: %w", repository.ErrNotFound)
	}
	return from, to, nil
}
func (t *transaction) UpdateWalletBalance(ctx context.Context, id string, balance int64, at time.Time) error {
	if balance < 0 {
		return fmt.Errorf("wallet balance must not be negative")
	}
	return t.update(ctx, "update wallet balance", `UPDATE wallets SET balance=$2, updated_at=$3 WHERE id=$1`, repository.ErrNotFound, id, balance, at)
}
