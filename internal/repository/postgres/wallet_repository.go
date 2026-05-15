package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/trjade/wallet-transfer-service/internal/domain"
)

type WalletRepository struct {
	db *pgxpool.Pool
}

func NewWalletRepository(db *pgxpool.Pool) *WalletRepository {
	return &WalletRepository{db: db}
}

func (r *WalletRepository) getExecutor(ctx context.Context) QueryExecutor {
	tx := extractTx(ctx)
	if tx != nil {
		return tx
	}

	return r.db
}

func (r *WalletRepository) GetWalletByID(ctx context.Context, walletID uuid.UUID) (*domain.Wallet, error) {
	var wallet domain.Wallet
	executor := r.getExecutor(ctx)

	query := `
		SELECT
			id,
			balance,
			created_at,
			updated_at
		FROM wallets
		WHERE id = $1
	`

	err := executor.QueryRow(ctx, query, walletID).Scan(&wallet.ID, &wallet.Balance, &wallet.CreatedAt, &wallet.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &wallet, nil
}

func (r *WalletRepository) GetWalletsForUpdate(ctx context.Context, walletIDs []uuid.UUID) ([]domain.Wallet, error) {
	query := `
		SELECT
			id,
			balance,
			created_at,
			updated_at
		FROM wallets
		WHERE id = ANY($1)
		ORDER BY id
		FOR UPDATE
	`

	executor := r.getExecutor(ctx)
	rows, err := executor.Query(ctx, query, walletIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var wallets []domain.Wallet

	for rows.Next() {
		var wallet domain.Wallet
		if err := rows.Scan(&wallet.ID, &wallet.Balance, &wallet.CreatedAt, &wallet.UpdatedAt); err != nil {
			return nil, err
		}
		wallets = append(wallets, wallet)
	}

	return wallets, rows.Err()
}

func (r *WalletRepository) UpdateWallet(ctx context.Context, walletID uuid.UUID, newBalance int64) error {
	query := `
		UPDATE wallets
		SET balance = $2, updated_at = NOW()
		WHERE id = $1
	`
	executor := r.getExecutor(ctx)
	_, err := executor.Exec(ctx, query, walletID, newBalance)

	return err
}
