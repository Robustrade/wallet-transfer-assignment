package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/domain"
)

type walletRepo struct{ db dbtx }

func (r *walletRepo) Create(ctx context.Context, w *domain.Wallet) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO wallets (id, balance, opening_balance, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		w.ID, w.Balance, w.OpeningBalance, w.CreatedAt, w.UpdatedAt)
	if isPgCode(err, codeUniqueViolation) {
		return domain.ErrWalletExists
	}
	if err != nil {
		return fmt.Errorf("insert wallet: %w", err)
	}
	return nil
}

func (r *walletRepo) Get(ctx context.Context, id string) (*domain.Wallet, error) {
	var w domain.Wallet
	err := r.db.QueryRow(ctx,
		`SELECT id, balance, opening_balance, created_at, updated_at FROM wallets WHERE id = $1`, id).
		Scan(&w.ID, &w.Balance, &w.OpeningBalance, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}
	w.CreatedAt, w.UpdatedAt = w.CreatedAt.UTC(), w.UpdatedAt.UTC()
	return &w, nil
}

// LockForUpdate takes row locks in ascending id order. ORDER BY is applied
// before the row-locking step, so every transaction acquires locks in the same
// global order and opposite-direction transfers cannot deadlock.
func (r *walletRepo) LockForUpdate(ctx context.Context, ids ...string) (map[string]*domain.Wallet, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, balance, opening_balance, created_at, updated_at
		   FROM wallets
		  WHERE id = ANY($1)
		  ORDER BY id
		    FOR UPDATE`, ids)
	if err != nil {
		return nil, fmt.Errorf("lock wallets: %w", err)
	}
	defer rows.Close()

	wallets := make(map[string]*domain.Wallet, len(ids))
	for rows.Next() {
		var w domain.Wallet
		if err := rows.Scan(&w.ID, &w.Balance, &w.OpeningBalance, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan wallet: %w", err)
		}
		w.CreatedAt, w.UpdatedAt = w.CreatedAt.UTC(), w.UpdatedAt.UTC()
		wallets[w.ID] = &w
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lock wallets: %w", err)
	}
	return wallets, nil
}

func (r *walletRepo) UpdateBalance(ctx context.Context, w *domain.Wallet, now time.Time) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE wallets SET balance = $2, updated_at = $3 WHERE id = $1`, w.ID, w.Balance, now)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrWalletNotFound
	}
	w.UpdatedAt = now
	return nil
}
