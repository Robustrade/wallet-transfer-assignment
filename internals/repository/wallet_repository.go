package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrWalletNotFound = errors.New("wallet not found")

type pgxWalletRepository struct{}

type WalletRepository interface {
	// GetForUpdate reads a wallet row with SELECT ... FOR UPDATE, locking it
	// for the duration of the enclosing transaction.
	GetForUpdate(ctx context.Context, tx pgx.Tx, walletID uuid.UUID) (*models.Wallet, error)
	// UpdateBalance persists the wallet's new balance.
	UpdateBalance(ctx context.Context, tx pgx.Tx, wallet *models.Wallet) error
}

func NewWalletRepository() WalletRepository {
	return &pgxWalletRepository{}
}

// GetForUpdate locks the wallet row for the duration of the caller's
// transaction using SELECT ... FOR UPDATE. Any other transaction trying to
// lock the same row will block until this one commits or rolls back — this
// is what serializes concurrent transfers touching the same wallet.
func (r *pgxWalletRepository) GetForUpdate(ctx context.Context, tx pgx.Tx, walletID uuid.UUID) (*models.Wallet, error) {
	// Used parameter placeholders so query is safe from SQL Injection attacks.
	const query = `
		SELECT id, owner_name, balance, version
		FROM wallets
		WHERE id = $1
		FOR UPDATE
		`

	var w models.Wallet
	err := tx.QueryRow(ctx, query, walletID).Scan(&w.ID, &w.OwnerName, &w.Balance, &w.Version)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, fmt.Errorf("locking wallet %s: %w", walletID, err)
	}
	return &w, nil
}

// UpdateBalance writes the wallet's new balance and bumps its version.
// Because the row is already locked by GetForUpdate within the same
// transaction, this is safe from lost-update races.
func (r *pgxWalletRepository) UpdateBalance(ctx context.Context, tx pgx.Tx, wallet *models.Wallet) error {
	// Used parameter placeholders so query is safe from SQL Injection attacks.
	const query = `
		UPDATE wallets
		SET balance = $1, version = version + 1, updated_at = now()
		WHERE id = $2`

	tag, err := tx.Exec(ctx, query, wallet.Balance, wallet.ID)
	if err != nil {
		return fmt.Errorf("updating wallet %s balance: %w", wallet.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrWalletNotFound
	}
	return nil
}
