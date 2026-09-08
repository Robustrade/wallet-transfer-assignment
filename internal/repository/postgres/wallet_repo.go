package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
)

// WalletRepository persists wallets.
type WalletRepository struct{ *DB }

// NewWalletRepository wires the repository to a DB.
func NewWalletRepository(db *DB) *WalletRepository { return &WalletRepository{DB: db} }

const walletColumns = `id, balance, currency, version, created_at, updated_at`

// GetForUpdate reads a wallet and holds a row-level lock until the surrounding
// transaction ends. This is the mechanism that serialises concurrent debits of
// the same wallet.
func (r *WalletRepository) GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	row := r.q(ctx).QueryRow(ctx,
		`SELECT `+walletColumns+` FROM wallets WHERE id = $1 FOR UPDATE`, id)
	return scanWallet(row)
}

// Get reads a wallet without locking.
func (r *WalletRepository) Get(ctx context.Context, id string) (*domain.Wallet, error) {
	row := r.q(ctx).QueryRow(ctx,
		`SELECT `+walletColumns+` FROM wallets WHERE id = $1`, id)
	return scanWallet(row)
}

// UpdateBalance persists the new balance and bumps the optimistic version.
func (r *WalletRepository) UpdateBalance(ctx context.Context, w *domain.Wallet) error {
	tag, err := r.q(ctx).Exec(ctx,
		`UPDATE wallets
		    SET balance = $1, version = version + 1, updated_at = now()
		  WHERE id = $2`,
		w.Balance.Amount, w.ID)
	if err != nil {
		return fmt.Errorf("postgres: update wallet balance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrWalletNotFound
	}
	w.Version++
	return nil
}

func scanWallet(row pgx.Row) (*domain.Wallet, error) {
	var (
		w        domain.Wallet
		balance  int64
		currency string
	)
	if err := row.Scan(&w.ID, &balance, &currency, &w.Version, &w.CreatedAt, &w.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("postgres: scan wallet: %w", err)
	}
	w.Balance = domain.Money{Amount: balance, Currency: currency}
	return &w, nil
}
