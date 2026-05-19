package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
)

const transferColumns = `
	transfer_id, source_wallet_id, target_wallet_id, amount, currency, state, created_at, updated_at
`

// TransferRepository implements repository.TransferRepository.
type TransferRepository struct {
	db *sql.DB
}

// Create inserts a new transfer row.
func (r *TransferRepository) Create(ctx context.Context, transfer *domain.Transfer) error {
	exec := dbOrTx(ctx, r.db)
	const query = `
		INSERT INTO transfers (
			transfer_id,
			source_wallet_id,
			target_wallet_id,
			amount,
			currency,
			state
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at
	`

	row := exec.QueryRowContext(
		ctx,
		query,
		transfer.ID,
		transfer.SourceWalletID,
		transfer.TargetWalletID,
		transfer.Amount,
		transfer.Currency,
		string(transfer.State),
	)
	if err := row.Scan(&transfer.CreatedAt, &transfer.UpdatedAt); err != nil {
		return fmt.Errorf("insert transfer: %w", err)
	}
	return nil
}

// GetByID loads a transfer by primary key.
func (r *TransferRepository) GetByID(ctx context.Context, transferID string) (*domain.Transfer, error) {
	exec := dbOrTx(ctx, r.db)
	query := `SELECT ` + transferColumns + ` FROM transfers WHERE transfer_id = $1`
	row := exec.QueryRowContext(ctx, query, transferID)
	return scanTransfer(row)
}

// UpdateState updates transfer state and updated_at.
func (r *TransferRepository) UpdateState(ctx context.Context, transferID string, state domain.TransferState) error {
	exec := dbOrTx(ctx, r.db)
	const query = `
		UPDATE transfers
		SET state = $1, updated_at = NOW()
		WHERE transfer_id = $2
	`

	result, err := exec.ExecContext(ctx, query, string(state), transferID)
	if err != nil {
		return fmt.Errorf("update transfer state: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update transfer state rows affected: %w", err)
	}
	if rows == 0 {
		return domain.ErrNotFound
	}
	return nil
}
