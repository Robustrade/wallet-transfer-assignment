package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/example/wallet-transfer-assignment/internal/domain"
)

type TransferRepo struct {
	db *sql.DB
}

func NewTransferRepo(db *sql.DB) *TransferRepo {
	return &TransferRepo{db: db}
}

func (r *TransferRepo) Create(ctx context.Context, tx *sql.Tx, transfer *domain.Transfer) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		transfer.ID, transfer.IdempotencyKey, transfer.FromWalletID, transfer.ToWalletID,
		transfer.Amount, string(transfer.State), transfer.CreatedAt, transfer.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create transfer: %w", err)
	}
	return nil
}

func (r *TransferRepo) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, created_at, updated_at
		 FROM transfers WHERE idempotency_key = $1`,
		key,
	)

	t := &domain.Transfer{}
	var state string
	err := row.Scan(&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &state, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get transfer by idempotency key: %w", err)
	}
	t.State = domain.TransferState(state)
	return t, nil
}

func (r *TransferRepo) UpdateState(ctx context.Context, tx *sql.Tx, id string, state domain.TransferState) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE transfers SET state = $1, updated_at = $2 WHERE id = $3`,
		string(state), time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("update transfer state: %w", err)
	}
	return nil
}

func (r *TransferRepo) CreateLedgerEntries(ctx context.Context, tx *sql.Tx, entries []*domain.LedgerEntry) error {
	for _, e := range entries {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO ledger_entries (wallet_id, transfer_id, entry_type, amount)
			 VALUES ($1, $2, $3, $4)`,
			e.WalletID, e.TransferID, e.EntryType, e.Amount,
		)
		if err != nil {
			return fmt.Errorf("create ledger entry: %w", err)
		}
	}
	return nil
}
