package repositories

import (
	"database/sql"
	"fmt"
	"wallet-transfer-service/internal/models"
)

type walletRepository struct {
	db *sql.DB
}

// NewWalletRepository creates a new wallet repository
func NewWalletRepository(db *sql.DB) WalletRepository {
	return &walletRepository{db: db}
}

// GetByID retrieves a wallet by ID
func (r *walletRepository) GetByID(tx *sql.Tx, id string) (*models.Wallet, error) {
	query := `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1`

	var wallet models.Wallet
	err := tx.QueryRow(query, id).Scan(
		&wallet.ID,
		&wallet.Balance,
		&wallet.CreatedAt,
		&wallet.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("wallet not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet: %w", err)
	}

	return &wallet, nil
}

// GetByIDForUpdate retrieves a wallet by ID with row-level lock (SELECT FOR UPDATE)
func (r *walletRepository) GetByIDForUpdate(tx *sql.Tx, id string) (*models.Wallet, error) {
	query := `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1 FOR UPDATE`

	var wallet models.Wallet
	err := tx.QueryRow(query, id).Scan(
		&wallet.ID,
		&wallet.Balance,
		&wallet.CreatedAt,
		&wallet.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("wallet not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to lock wallet: %w", err)
	}

	return &wallet, nil
}

// UpdateBalance updates wallet balance atomically
func (r *walletRepository) UpdateBalance(tx *sql.Tx, id string, amount int64, isCredit bool) error {
	var query string
	if isCredit {
		// Credit: increase balance
		query = `UPDATE wallets SET balance = balance + $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	} else {
		// Debit: decrease balance (with check)
		query = `UPDATE wallets SET balance = balance - $1, updated_at = CURRENT_TIMESTAMP
		         WHERE id = $2 AND balance >= $1`
	}

	result, err := tx.Exec(query, amount, id)
	if err != nil {
		return fmt.Errorf("failed to update balance: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		if !isCredit {
			return fmt.Errorf("insufficient balance")
		}
		return fmt.Errorf("wallet not found or balance update failed")
	}

	return nil
}

// Create creates a new wallet
func (r *walletRepository) Create(tx *sql.Tx, wallet *models.Wallet) error {
	query := `INSERT INTO wallets (id, balance, created_at, updated_at)
	          VALUES ($1, $2, $3, $4)`

	_, err := tx.Exec(query, wallet.ID, wallet.Balance, wallet.CreatedAt, wallet.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create wallet: %w", err)
	}

	return nil
}
