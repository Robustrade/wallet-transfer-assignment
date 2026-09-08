package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
)

// TransferRepository persists transfers.
type TransferRepository struct{ *DB }

// NewTransferRepository wires the repository to a DB.
func NewTransferRepository(db *DB) *TransferRepository { return &TransferRepository{DB: db} }

const transferColumns = `id, idempotency_key, from_wallet_id, to_wallet_id, amount, currency, state, COALESCE(failure_reason, ''), created_at, updated_at`

// Create inserts a new (PENDING) transfer.
func (r *TransferRepository) Create(ctx context.Context, t *domain.Transfer) error {
	err := r.q(ctx).QueryRow(ctx,
		`INSERT INTO transfers
		    (id, idempotency_key, from_wallet_id, to_wallet_id, amount, currency, state)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING created_at, updated_at`,
		t.ID, t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount.Amount, t.Amount.Currency, string(t.State)).
		Scan(&t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err, "transfers_idempotency_key_key") {
			return domain.ErrDuplicateIdempotencyKey
		}
		return fmt.Errorf("postgres: insert transfer: %w", err)
	}
	return nil
}

// GetByID loads a transfer by primary key.
func (r *TransferRepository) GetByID(ctx context.Context, id string) (*domain.Transfer, error) {
	row := r.q(ctx).QueryRow(ctx,
		`SELECT `+transferColumns+` FROM transfers WHERE id = $1`, id)
	return scanTransfer(row)
}

// GetByIdempotencyKey loads the transfer produced by an idempotency key.
func (r *TransferRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	row := r.q(ctx).QueryRow(ctx,
		`SELECT `+transferColumns+` FROM transfers WHERE idempotency_key = $1`, key)
	return scanTransfer(row)
}

// UpdateState persists a state change and failure reason.
func (r *TransferRepository) UpdateState(ctx context.Context, t *domain.Transfer) error {
	tag, err := r.q(ctx).Exec(ctx,
		`UPDATE transfers
		    SET state = $1, failure_reason = NULLIF($2, ''), updated_at = now()
		  WHERE id = $3`,
		string(t.State), t.FailureReason, t.ID)
	if err != nil {
		return fmt.Errorf("postgres: update transfer state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrTransferNotFound
	}
	return nil
}

func scanTransfer(row pgx.Row) (*domain.Transfer, error) {
	var (
		t        domain.Transfer
		amount   int64
		currency string
		state    string
	)
	if err := row.Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID,
		&amount, &currency, &state, &t.FailureReason, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, fmt.Errorf("postgres: scan transfer: %w", err)
	}
	t.Amount = domain.Money{Amount: amount, Currency: currency}
	t.State = domain.TransferState(state)
	return &t, nil
}
