package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/benjamin-benny/wallet-transfer/internal/domain"
)

type WalletRepository struct{}

// Create inserts a new wallet with the given starting balance and returns it.
func (r *WalletRepository) Create(ctx context.Context, tx pgx.Tx, currency string, initialBalance domain.Amount) (*domain.Wallet, error) {
	const q = `
		INSERT INTO wallets (currency, balance)
		VALUES ($1, $2)
		RETURNING id, balance, currency, created_at, updated_at`

	row := tx.QueryRow(ctx, q, currency, amountToNumeric(initialBalance))
	return scanWallet(row)
}

// GetByIDsForUpdate locks both wallets in UUID-sorted order to prevent deadlocks,
// then returns them in the same sorted order.
func (r *WalletRepository) GetByIDsForUpdate(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) ([]*domain.Wallet, error) {
	// Pass UUIDs as pgtype.UUID slice so pgx uses the UUID OID (not text).
	pgIDs := make([]pgtype.UUID, len(ids))
	for i, id := range ids {
		pgIDs[i] = pgtype.UUID{Bytes: id, Valid: true}
	}

	const q = `
		SELECT id, balance, currency, created_at, updated_at
		FROM wallets
		WHERE id = ANY($1)
		ORDER BY id
		FOR UPDATE`

	rows, err := tx.Query(ctx, q, pgIDs)
	if err != nil {
		return nil, fmt.Errorf("GetByIDsForUpdate query: %w", err)
	}
	defer rows.Close()

	var wallets []*domain.Wallet
	for rows.Next() {
		w, err := scanWallet(rows)
		if err != nil {
			return nil, fmt.Errorf("GetByIDsForUpdate scan: %w", err)
		}
		wallets = append(wallets, w)
	}
	return wallets, rows.Err()
}

// UpdateBalance sets a wallet's balance and bumps updated_at.
func (r *WalletRepository) UpdateBalance(ctx context.Context, tx pgx.Tx, id uuid.UUID, balance domain.Amount) error {
	const q = `UPDATE wallets SET balance = $1, updated_at = NOW() WHERE id = $2`

	tag, err := tx.Exec(ctx, q, amountToNumeric(balance), pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		return fmt.Errorf("UpdateBalance exec: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("wallet %s vanished after FOR UPDATE: %w", id, domain.ErrInvariantViolation)
	}
	return nil
}

// scanWallet reads one wallet row from either pgx.Row or pgx.Rows.
func scanWallet(s interface {
	Scan(dest ...any) error
}) (*domain.Wallet, error) {
	var (
		pgID      pgtype.UUID
		pgBalance pgtype.Numeric
		w         domain.Wallet
	)
	err := s.Scan(&pgID, &pgBalance, &w.Currency, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, err
	}
	w.ID = uuid.UUID(pgID.Bytes)
	w.Balance = numericToAmount(pgBalance)
	return &w, nil
}
