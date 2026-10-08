package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

func (t *transaction) CreateTransfer(ctx context.Context, v domain.Transfer) error {
	if err := v.Validate(); err != nil {
		return fmt.Errorf("create transfer: %w", err)
	}
	if v.Status() != domain.TransferStatusPending {
		return fmt.Errorf("create pending transfer: %w", repository.ErrConflict)
	}
	_, err := t.exec(ctx, "create transfer", `INSERT INTO transfers (id,idempotency_key,from_wallet_id,to_wallet_id,amount,status,created_at,updated_at) VALUES ($1,NULLIF($2,''),$3,$4,$5,$6,$7,$8)`, v.ID, v.IdempotencyKey, v.FromWalletID, v.ToWalletID, v.Amount, v.Status(), v.CreatedAt, v.UpdatedAt)
	return err
}

// Reconstruct through validated domain transitions, without adding a status setter.
func scanTransfer(row *sql.Row) (domain.Transfer, error) {
	var id, from, to string
	var key sql.NullString
	var amount int64
	var status domain.TransferStatus
	var created, updated time.Time
	if err := row.Scan(&id, &key, &from, &to, &amount, &status, &created, &updated); err != nil {
		return domain.Transfer{}, err
	}
	return restoreTransfer(id, key.String, from, to, amount, status, created, updated)
}
func restoreTransfer(id, key, from, to string, amount int64, status domain.TransferStatus, created, updated time.Time) (domain.Transfer, error) {
	v, err := domain.NewTransfer(id, key, from, to, amount, created)
	if err != nil {
		return domain.Transfer{}, err
	}
	switch status {
	case domain.TransferStatusPending:
		v.UpdatedAt = updated
	case domain.TransferStatusProcessed:
		err = v.MarkProcessed(updated)
	case domain.TransferStatusFailed:
		err = v.MarkFailed(updated)
	default:
		err = status.Validate()
	}
	if err != nil {
		return domain.Transfer{}, err
	}
	return v, nil
}
func (t *transaction) GetTransfer(ctx context.Context, id string) (domain.Transfer, error) {
	if t.failure != nil {
		return domain.Transfer{}, t.failure
	}
	v, err := scanTransfer(t.tx.QueryRowContext(ctx, `SELECT id,idempotency_key,from_wallet_id,to_wallet_id,amount,status,created_at,updated_at FROM transfers WHERE id=$1`, id))
	return v, t.record("get transfer", err)
}
func (t *transaction) GetTransferByIdempotencyKey(ctx context.Context, key string) (domain.Transfer, error) {
	if t.failure != nil {
		return domain.Transfer{}, t.failure
	}
	v, err := scanTransfer(t.tx.QueryRowContext(ctx, `SELECT t.id,t.idempotency_key,t.from_wallet_id,t.to_wallet_id,t.amount,t.status,t.created_at,t.updated_at FROM transfers t JOIN idempotency_records i ON i.transfer_id=t.id AND i.idempotency_key=t.idempotency_key WHERE i.idempotency_key=$1`, key))
	return v, t.record("get idempotent transfer", err)
}
func (t *transaction) MarkTransferProcessed(ctx context.Context, id string, at time.Time) error {
	return t.markTransfer(ctx, id, domain.TransferStatusProcessed, at)
}
func (t *transaction) MarkTransferFailed(ctx context.Context, id string, at time.Time) error {
	return t.markTransfer(ctx, id, domain.TransferStatusFailed, at)
}
func (t *transaction) markTransfer(ctx context.Context, id string, status domain.TransferStatus, at time.Time) error {
	return t.update(ctx, "transition transfer", `UPDATE transfers SET status=$2, updated_at=$3 WHERE id=$1 AND status='PENDING'`, repository.ErrConflict, id, status, at)
}
