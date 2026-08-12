package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
)

type transferRepository struct {
	q dbtx
}

// InsertIfAbsent performs the speculative insert described in
// docs/design.md §6: if a concurrent transaction holds the same
// idempotency_key uncommitted, this statement blocks until that transaction
// resolves, then either observes the committed row (inserted=false) or finds
// the key free again and inserts (inserted=true) if it rolled back.
func (r *transferRepository) InsertIfAbsent(ctx context.Context, t domain.Transfer) (bool, error) {
	tag, err := r.q.Exec(ctx, `
		INSERT INTO transfers (id, idempotency_key, request_hash, from_wallet_id, to_wallet_id, amount_minor, currency, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (idempotency_key) DO NOTHING`,
		t.ID, t.IdempotencyKey, t.RequestHash, t.FromWalletID, t.ToWalletID, t.AmountMinor, t.Currency, string(t.Status))
	if err != nil {
		return false, translateError(err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *transferRepository) FindByID(ctx context.Context, id uuid.UUID) (domain.Transfer, error) {
	return r.scanOne(ctx, `
		SELECT id, idempotency_key, request_hash, from_wallet_id, to_wallet_id, amount_minor, currency, status,
		       failure_code, failure_message, created_at, updated_at, settled_at
		FROM transfers WHERE id = $1`, id)
}

func (r *transferRepository) FindByIdempotencyKey(ctx context.Context, key string) (domain.Transfer, error) {
	return r.scanOne(ctx, `
		SELECT id, idempotency_key, request_hash, from_wallet_id, to_wallet_id, amount_minor, currency, status,
		       failure_code, failure_message, created_at, updated_at, settled_at
		FROM transfers WHERE idempotency_key = $1`, key)
}

func (r *transferRepository) scanOne(ctx context.Context, sql string, arg any) (domain.Transfer, error) {
	row := r.q.QueryRow(ctx, sql, arg)
	var t domain.Transfer
	var status string
	var failureCode, failureMessage *string
	var settledAt *time.Time
	if err := row.Scan(&t.ID, &t.IdempotencyKey, &t.RequestHash, &t.FromWalletID, &t.ToWalletID,
		&t.AmountMinor, &t.Currency, &status, &failureCode, &failureMessage, &t.CreatedAt, &t.UpdatedAt, &settledAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Transfer{}, domain.ErrTransferNotFound
		}
		return domain.Transfer{}, translateError(err)
	}
	t.Status = domain.TransferStatus(status)
	if failureCode != nil {
		t.FailureCode = domain.FailureCode(*failureCode)
	}
	if failureMessage != nil {
		t.FailureMessage = *failureMessage
	}
	t.SettledAt = settledAt
	return t, nil
}

// MarkProcessed and MarkFailed are both guarded by WHERE status = 'PENDING',
// backstopped by the transfers_enforce_invariants trigger. RowsAffected() !=
// 1 means the transfer was not PENDING — an invariant violation the caller
// must treat as an error, not silently ignore.
func (r *transferRepository) MarkProcessed(ctx context.Context, id uuid.UUID, settledAt time.Time) error {
	tag, err := r.q.Exec(ctx, `
		UPDATE transfers SET status = 'PROCESSED', settled_at = $2, updated_at = now()
		WHERE id = $1 AND status = 'PENDING'`, id, settledAt)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: transfer %s was not PENDING when marking processed", domain.ErrIllegalStateChange, id)
	}
	return nil
}

func (r *transferRepository) MarkFailed(ctx context.Context, id uuid.UUID, code domain.FailureCode, message string) error {
	tag, err := r.q.Exec(ctx, `
		UPDATE transfers SET status = 'FAILED', failure_code = $2, failure_message = $3, updated_at = now()
		WHERE id = $1 AND status = 'PENDING'`, id, string(code), message)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: transfer %s was not PENDING when marking failed", domain.ErrIllegalStateChange, id)
	}
	return nil
}
