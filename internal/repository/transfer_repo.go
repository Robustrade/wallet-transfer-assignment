package repository

import (
	"context"
	"errors"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// transferRepo implements TransferRepository using PostgreSQL.
type transferRepo struct{}

// NewTransferRepository creates a new PostgreSQL transfer repository.
func NewTransferRepository() TransferRepository {
	return &transferRepo{}
}

// Create inserts a new transfer. If the idempotency key already exists,
// it returns the existing transfer and isDuplicate=true.
func (r *transferRepo) Create(ctx context.Context, tx pgx.Tx, transfer *domain.Transfer) (*domain.Transfer, bool, error) {
	// First check if transfer with this idempotency key already exists
	existing, err := r.GetByIdempotencyKey(ctx, tx, transfer.IdempotencyKey)
	if err == nil {
		return existing, true, nil
	}
	if !errors.Is(err, domain.ErrTransferNotFound) {
		return nil, false, err
	}

	// Use SAVEPOINT so a potential unique constraint violation doesn't abort the transaction block
	if tx != nil {
		_, _ = tx.Exec(ctx, "SAVEPOINT sp_create_transfer")
	}

	var id string
	err = tx.QueryRow(ctx,
		`INSERT INTO transfers (idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id`,
		transfer.IdempotencyKey, transfer.FromWalletID, transfer.ToWalletID,
		transfer.Amount, transfer.Status, transfer.CreatedAt, transfer.UpdatedAt,
	).Scan(&id)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if tx != nil {
				_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT sp_create_transfer")
			}
			existing, lookupErr := r.GetByIdempotencyKey(ctx, tx, transfer.IdempotencyKey)
			if lookupErr != nil {
				return nil, false, lookupErr
			}
			return existing, true, nil
		}
		if tx != nil {
			_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT sp_create_transfer")
		}
		return nil, false, err
	}

	if tx != nil {
		_, _ = tx.Exec(ctx, "RELEASE SAVEPOINT sp_create_transfer")
	}

	transfer.ID = id
	return transfer, false, nil
}

func (r *transferRepo) GetByID(ctx context.Context, tx pgx.Tx, id string) (*domain.Transfer, error) {
	t := &domain.Transfer{}
	var failureReason *string
	err := tx.QueryRow(ctx,
		`SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		 FROM transfers WHERE id = $1`,
		id,
	).Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID,
		&t.Amount, &t.Status, &failureReason, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, err
	}
	if failureReason != nil {
		t.FailureReason = *failureReason
	}
	return t, nil
}

func (r *transferRepo) GetByIdempotencyKey(ctx context.Context, tx pgx.Tx, key string) (*domain.Transfer, error) {
	t := &domain.Transfer{}
	var failureReason *string
	err := tx.QueryRow(ctx,
		`SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		 FROM transfers WHERE idempotency_key = $1`,
		key,
	).Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID,
		&t.Amount, &t.Status, &failureReason, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, err
	}
	if failureReason != nil {
		t.FailureReason = *failureReason
	}
	return t, nil
}

func (r *transferRepo) UpdateStatus(ctx context.Context, tx pgx.Tx, id string, status domain.TransferStatus, failureReason string) error {
	var cmdTag pgconn.CommandTag
	var err error

	if failureReason != "" {
		cmdTag, err = tx.Exec(ctx,
			`UPDATE transfers SET status = $1, failure_reason = $2, updated_at = NOW() WHERE id = $3`,
			status, failureReason, id,
		)
	} else {
		cmdTag, err = tx.Exec(ctx,
			`UPDATE transfers SET status = $1, updated_at = NOW() WHERE id = $2`,
			status, id,
		)
	}

	if err != nil {
		return err
	}
	if cmdTag.RowsAffected() == 0 {
		return domain.ErrTransferNotFound
	}
	return nil
}
