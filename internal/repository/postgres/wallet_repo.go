package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"wallet-transfer-assignment/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type WalletRepository struct {
	pool *pgxpool.Pool
}

func NewWalletRepository(pool *pgxpool.Pool) *WalletRepository {
	return &WalletRepository{pool: pool}
}

func (r *WalletRepository) CreateWallet(ctx context.Context, wallet *domain.Wallet) error {
	query := `
		INSERT INTO wallets (id, balance, currency, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := r.pool.Exec(ctx, query,
		wallet.ID,
		wallet.Balance.Decimal(),
		wallet.Currency,
		wallet.CreatedAt,
		wallet.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed creating wallet: %w", err)
	}
	return nil
}

func (r *WalletRepository) GetWalletByID(ctx context.Context, id uuid.UUID) (*domain.Wallet, error) {
	query := `
		SELECT id, balance, currency, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`
	var w domain.Wallet
	var balanceDec decimal.Decimal

	err := r.pool.QueryRow(ctx, query, id).Scan(
		&w.ID,
		&balanceDec,
		&w.Currency,
		&w.CreatedAt,
		&w.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound(id.String())
		}
		return nil, domain.ErrInternal(fmt.Errorf("failed fetching wallet by id: %w", err))
	}
	w.Balance = domain.NewMoney(balanceDec)
	return &w, nil
}

// LockAndGetWallets acquires row-level locks (SELECT FOR UPDATE) on both wallets inside tx in deterministic UUID order to prevent deadlocks.
func (r *WalletRepository) LockAndGetWallets(ctx context.Context, tx pgx.Tx, fromID, toID uuid.UUID) (fromWallet, toWallet *domain.Wallet, err error) {
	// Deterministic lock order: smallest UUID first, largest second
	firstID, secondID := fromID, toID
	if firstID.String() > secondID.String() {
		firstID, secondID = toID, fromID
	}

	query := `
		SELECT id, balance, currency, created_at, updated_at
		FROM wallets
		WHERE id IN ($1, $2)
		ORDER BY id ASC
		FOR UPDATE
	`

	rows, err := tx.Query(ctx, query, firstID, secondID)
	if err != nil {
		return nil, nil, domain.ErrInternal(fmt.Errorf("failed executing SELECT FOR UPDATE on wallets: %w", err))
	}
	defer rows.Close()

	walletsMap := make(map[uuid.UUID]*domain.Wallet, 2)
	for rows.Next() {
		var w domain.Wallet
		var balanceDec decimal.Decimal
		if err := rows.Scan(&w.ID, &balanceDec, &w.Currency, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, nil, domain.ErrInternal(fmt.Errorf("failed scanning wallet row: %w", err))
		}
		w.Balance = domain.NewMoney(balanceDec)
		walletsMap[w.ID] = &w
	}

	if err := rows.Err(); err != nil {
		return nil, nil, domain.ErrInternal(fmt.Errorf("rows iteration error: %w", err))
	}

	fw, existsFrom := walletsMap[fromID]
	if !existsFrom {
		return nil, nil, domain.ErrWalletNotFound(fromID.String())
	}

	tw, existsTo := walletsMap[toID]
	if !existsTo {
		return nil, nil, domain.ErrWalletNotFound(toID.String())
	}

	return fw, tw, nil
}

func (r *WalletRepository) UpdateWalletBalance(ctx context.Context, tx pgx.Tx, wallet *domain.Wallet) error {
	query := `
		UPDATE wallets
		SET balance = $1, updated_at = $2
		WHERE id = $3
	`
	now := time.Now().UTC()
	wallet.UpdatedAt = now
	res, err := tx.Exec(ctx, query, wallet.Balance.Decimal(), wallet.UpdatedAt, wallet.ID)
	if err != nil {
		return domain.ErrInternal(fmt.Errorf("failed updating wallet balance: %w", err))
	}
	if res.RowsAffected() == 0 {
		return domain.ErrWalletNotFound(wallet.ID.String())
	}
	return nil
}
