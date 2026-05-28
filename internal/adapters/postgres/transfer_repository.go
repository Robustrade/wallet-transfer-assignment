package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/transfers"
)

type TransferRepository struct {
	pool *pgxpool.Pool
}

func NewTransferRepository(pool *pgxpool.Pool) *TransferRepository {
	return &TransferRepository{pool: pool}
}

func (r *TransferRepository) InsertTx(ctx context.Context, tx pgx.Tx, t transfers.Transfer) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO transfers
         (transfer_id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, failure_reason)
         VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		t.ID, t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount, string(t.State), t.FailureReason,
	)
	return err
}

func (r *TransferRepository) UpdateStateTx(ctx context.Context, tx pgx.Tx, transferID string, state transfers.State, failureReason string) error {
	_, err := tx.Exec(ctx,
		`UPDATE transfers
         SET state = $2, failure_reason = $3, updated_at = now()
         WHERE transfer_id = $1`,
		transferID, string(state), failureReason,
	)
	return err
}
