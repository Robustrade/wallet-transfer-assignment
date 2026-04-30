package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	apperr "github.com/Robustrade/wallet-transfer-assignment/common/error"
	"github.com/Robustrade/wallet-transfer-assignment/common/util/money"
	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/model"
	repoiface "github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/repo/iface"
)

// transferRepository is the GORM-backed implementation of TransferRepoIface.
type transferRepository struct {
	db *gorm.DB
}

// NewTransferRepository constructs a transfer repository.
func NewTransferRepository(db *gorm.DB) repoiface.TransferRepoIface {
	return &transferRepository{db: db}
}

// Claim attempts to insert the supplied transfer in PENDING state. On a
// unique-key conflict, it loads the existing row.
//
// The Postgres unique-index xmax lock causes concurrent attempts on the same
// idempotency_key to block until the first transaction commits or rolls back,
// so the SELECT after a conflict reads committed state.
func (r *transferRepository) Claim(ctx context.Context, t model.Transfer) (bool, *model.Transfer, error) {
	q, err := mustTxQuerier(ctx)
	if err != nil {
		return false, nil, err
	}

	const insertSQL = `
INSERT INTO transfers (
    id, idempotency_key, request_hash,
    from_wallet_id, to_wallet_id, amount_minor, currency, status
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING created_at, updated_at`

	var createdAt, updatedAt time.Time
	scanErr := q.Raw(
		insertSQL,
		t.ID, t.IdempotencyKey, t.RequestHash,
		t.FromWalletID, t.ToWalletID, t.Amount.Minor(), t.Currency, string(t.Status),
	).Row().Scan(&createdAt, &updatedAt)

	switch {
	case scanErr == nil:
		t.CreatedAt = createdAt
		t.UpdatedAt = updatedAt
		return true, &t, nil
	case errors.Is(scanErr, sql.ErrNoRows):
		// Conflict path: idempotency key already claimed; fetch the existing row.
		existing, err := r.getByKey(ctx, q, t.IdempotencyKey)
		if err != nil {
			return false, nil, err
		}
		return false, existing, nil
	case isPgConstraintViolation(scanErr, sqlstateForeignKeyViolation, constraintTransfersFromWalletFKey),
		isPgConstraintViolation(scanErr, sqlstateForeignKeyViolation, constraintTransfersToWalletFKey):
		// Pin the mapping to the two wallet-referencing FKs. If a future
		// migration adds another FK from `transfers` (e.g. to `accounts`),
		// a violation there should surface as a 5xx rather than be
		// misclassified as a missing wallet.
		return false, nil, apperr.ErrWalletNotFound.WithWrap(scanErr)
	default:
		return false, nil, fmt.Errorf("transfer_repo: insert: %w", scanErr)
	}
}

// UpdateOutcome moves a transfer from PENDING to a terminal state and writes
// the canonical currency. Returns the new updated_at so the caller can refresh
// any in-memory copy of the transfer.
//
// The `status = 'PENDING'` predicate cements the PENDING -> terminal state
// machine at the storage layer: a row already in PROCESSED / FAILED is left
// untouched and the call surfaces as ErrNoRows. Today the call graph keeps
// this from happening (only the in-flight transaction touches the row), but
// the guard closes the door against a future caller that reuses the method.
func (r *transferRepository) UpdateOutcome(
	ctx context.Context,
	id uuid.UUID,
	status model.TransferStatus,
	currency string,
	failureReason *string,
) (time.Time, error) {
	q, err := mustTxQuerier(ctx)
	if err != nil {
		return time.Time{}, err
	}

	const sqlText = `
UPDATE transfers
SET status         = $1,
    currency       = $2,
    failure_reason = $3,
    updated_at     = NOW()
WHERE id = $4 AND status = 'PENDING'
RETURNING updated_at`

	var updatedAt time.Time
	scanErr := q.Raw(sqlText, string(status), currency, failureReason, id).Row().Scan(&updatedAt)
	switch {
	case scanErr == nil:
		return updatedAt, nil
	case errors.Is(scanErr, sql.ErrNoRows):
		return time.Time{}, fmt.Errorf("transfer_repo: transfer %s not in PENDING state", id)
	default:
		return time.Time{}, fmt.Errorf("transfer_repo: update outcome: %w", scanErr)
	}
}

func (r *transferRepository) getByKey(_ context.Context, q *gorm.DB, key string) (*model.Transfer, error) {
	const sqlText = `
SELECT id, idempotency_key, request_hash,
       from_wallet_id, to_wallet_id, amount_minor, currency,
       status, failure_reason, created_at, updated_at
FROM transfers
WHERE idempotency_key = $1`

	var (
		t      model.Transfer
		amount int64
		status string
		reason sql.NullString
	)
	if err := q.Raw(sqlText, key).Row().Scan(
		&t.ID, &t.IdempotencyKey, &t.RequestHash,
		&t.FromWalletID, &t.ToWalletID, &amount, &t.Currency,
		&status, &reason, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("transfer_repo: select existing: %w", err)
	}
	t.Amount = money.FromMinor(amount)
	t.Status = model.TransferStatus(status)
	if reason.Valid {
		s := reason.String
		t.FailureReason = &s
	}
	return &t, nil
}
