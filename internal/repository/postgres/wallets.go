package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

type walletRepo struct{ q querier }

const selectWallet = `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1`

func (r walletRepo) Get(ctx context.Context, id string) (domain.Wallet, error) {
	return r.scanOne(ctx, selectWallet, id)
}

func (r walletRepo) GetForUpdate(ctx context.Context, id string) (domain.Wallet, error) {
	return r.scanOne(ctx, selectWallet+` FOR UPDATE`, id)
}

func (r walletRepo) Debit(ctx context.Context, id string, amount int64) error {
	return r.adjust(ctx, "debit", id, amount, -amount)
}

func (r walletRepo) Credit(ctx context.Context, id string, amount int64) error {
	return r.adjust(ctx, "credit", id, amount, amount)
}

// adjust applies delta in one statement: the read-modify-write happens inside
// Postgres, so it cannot lose a concurrent update, and CHECK (balance >= 0)
// rejects any overdraw even if a caller skipped the row lock.
func (r walletRepo) adjust(ctx context.Context, op, id string, amount, delta int64) error {
	if amount <= 0 {
		return fmt.Errorf("%s wallet %s: amount must be positive, got %d", op, id, amount)
	}
	tag, err := r.q.Exec(ctx,
		`UPDATE wallets SET balance = balance + $2, updated_at = now() WHERE id = $1`, id, delta)
	if err != nil {
		return translate(fmt.Errorf("%s wallet %s: %w", op, id, err))
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%s wallet %s: %w", op, id, domain.ErrWalletNotFound)
	}
	return nil
}

func (r walletRepo) scanOne(ctx context.Context, sql, id string) (domain.Wallet, error) {
	var w domain.Wallet
	err := r.q.QueryRow(ctx, sql, id).Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Wallet{}, fmt.Errorf("wallet %s: %w", id, domain.ErrWalletNotFound)
	}
	if err != nil {
		return domain.Wallet{}, translate(fmt.Errorf("load wallet %s: %w", id, err))
	}
	return w, nil
}
