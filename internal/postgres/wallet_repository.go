package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
)

type walletRepository struct {
	q dbtx
}

func (r *walletRepository) Get(ctx context.Context, id string) (domain.Wallet, error) {
	return r.scanOne(ctx, `
		SELECT id, kind, currency, balance_minor, created_at, updated_at
		FROM wallets WHERE id = $1`, id)
}

func (r *walletRepository) GetForUpdate(ctx context.Context, id string) (domain.Wallet, error) {
	// FOR NO KEY UPDATE, not FOR UPDATE — see docs/design.md §7 for why:
	// inserting a transfer row takes an implicit FOR KEY SHARE lock on both
	// wallets for the foreign keys, and FOR UPDATE conflicts with that in a
	// way that can deadlock even with a sorted lock-acquisition order.
	return r.scanOne(ctx, `
		SELECT id, kind, currency, balance_minor, created_at, updated_at
		FROM wallets WHERE id = $1 FOR NO KEY UPDATE`, id)
}

func (r *walletRepository) scanOne(ctx context.Context, sql string, id string) (domain.Wallet, error) {
	row := r.q.QueryRow(ctx, sql, id)
	var w domain.Wallet
	var kind string
	if err := row.Scan(&w.ID, &kind, &w.Currency, &w.BalanceMinor, &w.CreatedAt, &w.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Wallet{}, domain.ErrWalletNotFound
		}
		return domain.Wallet{}, translateError(err)
	}
	w.Kind = domain.WalletKind(kind)
	return w, nil
}

func (r *walletRepository) Create(ctx context.Context, w domain.Wallet) error {
	_, err := r.q.Exec(ctx, `
		INSERT INTO wallets (id, kind, currency, balance_minor)
		VALUES ($1, $2, $3, $4)`,
		w.ID, string(w.Kind), w.Currency, w.BalanceMinor)
	if err != nil {
		return translateError(err)
	}
	return nil
}

// ApplyDelta is the second of the three defence layers against negative
// balances described in docs/design.md §7 (the first being the locked
// balance check the service performs before calling this; the third is the
// database CHECK constraint). Zero rows returned means the guard clause
// rejected the update.
func (r *walletRepository) ApplyDelta(ctx context.Context, id string, delta int64) (int64, error) {
	row := r.q.QueryRow(ctx, `
		UPDATE wallets
		   SET balance_minor = balance_minor + $2, updated_at = now()
		 WHERE id = $1
		   AND (kind = 'SYSTEM' OR balance_minor + $2 >= 0)
		RETURNING balance_minor`,
		id, delta)
	var newBalance int64
	if err := row.Scan(&newBalance); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if _, getErr := r.Get(ctx, id); errors.Is(getErr, domain.ErrWalletNotFound) {
				return 0, domain.ErrWalletNotFound
			}
			return 0, domain.ErrInsufficientFunds
		}
		return 0, translateError(err)
	}
	return newBalance, nil
}
