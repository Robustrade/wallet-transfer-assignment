package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/helpers"
	"github.com/Robustrade/wallet-transfer-assignment/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WalletRepository is the DB access for wallets. Business rules live in service.
type WalletRepository struct {
	pool *pgxpool.Pool
}

func NewWalletRepository(pool *pgxpool.Pool) *WalletRepository {
	return &WalletRepository{pool: pool}
}

// Create inserts a wallet. Returns model.ErrConflict when the id already exists.
func (r *WalletRepository) Create(ctx context.Context, w *model.Wallet) error {
	q := QuerierFrom(ctx, r.pool)
	tag, err := q.Exec(ctx, `
		INSERT INTO wallets (id, opening_balance, balance, created_at, updated_at)
		VALUES (@id, @opening_balance, @balance, @created_at, @updated_at)
		ON CONFLICT (id) DO NOTHING`,
		pgx.NamedArgs{
			"id":              w.ID,
			"opening_balance": w.OpeningBalance.Int64(),
			"balance":         w.Balance.Int64(),
			"created_at":      w.CreatedAt,
			"updated_at":      w.UpdatedAt,
		},
	)
	if err != nil {
		return fmt.Errorf("insert wallet: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrConflict
	}
	return nil
}

// GetByID loads a wallet or model.ErrNotFound.
func (r *WalletRepository) GetByID(ctx context.Context, id string) (*model.Wallet, error) {
	q := QuerierFrom(ctx, r.pool)
	var w model.Wallet
	var opening, balance int64
	err := q.QueryRow(ctx, `
		SELECT id, opening_balance, balance, created_at, updated_at
		FROM wallets WHERE id = @id`,
		pgx.NamedArgs{"id": id},
	).Scan(&w.ID, &opening, &balance, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}
	w.OpeningBalance = helpers.Money(opening)
	w.Balance = helpers.Money(balance)
	return &w, nil
}

// LockByIDs SELECT … FOR UPDATE on the given ids, ordered by id.
func (r *WalletRepository) LockByIDs(ctx context.Context, ids ...string) ([]*model.Wallet, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	q := QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, opening_balance, balance, created_at, updated_at
		FROM wallets
		WHERE id = ANY(@ids)
		ORDER BY id
		FOR UPDATE`,
		pgx.NamedArgs{"ids": ids},
	)
	if err != nil {
		return nil, fmt.Errorf("lock wallets: %w", err)
	}
	defer rows.Close()

	var out []*model.Wallet
	for rows.Next() {
		var w model.Wallet
		var opening, balance int64
		if err := rows.Scan(&w.ID, &opening, &balance, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		w.OpeningBalance = helpers.Money(opening)
		w.Balance = helpers.Money(balance)
		out = append(out, &w)
	}
	return out, rows.Err()
}

// Debit runs UPDATE … WHERE balance >= amount. ok=false when no row matched.
func (r *WalletRepository) Debit(ctx context.Context, walletID string, amount helpers.Money) (bool, error) {
	q := QuerierFrom(ctx, r.pool)
	tag, err := q.Exec(ctx, `
		UPDATE wallets
		SET balance = balance - @amount, updated_at = @updated_at
		WHERE id = @id AND balance >= @amount`,
		pgx.NamedArgs{
			"id":         walletID,
			"amount":     amount.Int64(),
			"updated_at": time.Now().UTC(),
		},
	)
	if err != nil {
		return false, fmt.Errorf("debit wallet: %w", mapWalletWriteError(err))
	}
	return tag.RowsAffected() == 1, nil
}

// Credit runs UPDATE … SET balance = balance + amount.
func (r *WalletRepository) Credit(ctx context.Context, walletID string, amount helpers.Money) error {
	q := QuerierFrom(ctx, r.pool)
	tag, err := q.Exec(ctx, `
		UPDATE wallets
		SET balance = balance + @amount, updated_at = @updated_at
		WHERE id = @id`,
		pgx.NamedArgs{
			"id":         walletID,
			"amount":     amount.Int64(),
			"updated_at": time.Now().UTC(),
		},
	)
	if err != nil {
		return fmt.Errorf("credit wallet: %w", mapWalletWriteError(err))
	}
	if tag.RowsAffected() != 1 {
		return model.ErrNotFound
	}
	return nil
}

func mapWalletWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23514" &&
		strings.Contains(pgErr.Message, "opening_balance is immutable") {
		return model.ErrOpeningBalanceImmutable
	}
	return err
}
