package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"
)

// Store is the Postgres implementation of repository.Store.
type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// EnqueueTransfer inserts a PENDING transfer. ON CONFLICT does nothing for a
// duplicate key; when that happens we read the existing row and return it (created=false).
func (s *Store) EnqueueTransfer(ctx context.Context, p domain.NewTransferParams) (domain.Transfer, bool, error) {
	const insertSQL = `
		INSERT INTO transfer (idempotency_key, from_wallet_id, to_wallet_id, amount, state)
		VALUES ($1, $2, $3, $4, 'PENDING')
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING ` + transferCols

	t, err := scanTransfer(s.pool.QueryRow(ctx, insertSQL,
		p.IdempotencyKey, p.FromWalletID, p.ToWalletID, p.Amount))
	if err == nil {
		return t, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Transfer{}, false, classify(err)
	}

	// The key already exists, so read the saved transfer and return it.
	existing, err := s.getByKey(ctx, p.IdempotencyKey)
	if err != nil {
		return domain.Transfer{}, false, err
	}
	return existing, false, nil
}

func (s *Store) getByKey(ctx context.Context, key string) (domain.Transfer, error) {
	const q = `SELECT ` + transferCols + ` FROM transfer WHERE idempotency_key = $1`
	t, err := scanTransfer(s.pool.QueryRow(ctx, q, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Transfer{}, domain.ErrTransferNotFound
	}
	return t, err
}

func (s *Store) GetTransferByID(ctx context.Context, id uuid.UUID) (domain.Transfer, error) {
	const q = `SELECT ` + transferCols + ` FROM transfer WHERE id = $1`
	t, err := scanTransfer(s.pool.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Transfer{}, domain.ErrTransferNotFound
	}
	return t, err
}

// ClaimPending moves the oldest PENDING rows to PROCESSING for this worker. SKIP
// LOCKED lets many workers take different batches without waiting on each other.
func (s *Store) ClaimPending(ctx context.Context, workerID string, limit int) ([]domain.Transfer, error) {
	const q = `
		UPDATE transfer
		SET state = 'PROCESSING', claimed_at = now(), worker_id = $1, attempts = attempts + 1
		WHERE id IN (
			SELECT id FROM transfer
			WHERE state = 'PENDING'
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		RETURNING ` + transferCols

	rows, err := s.pool.Query(ctx, q, workerID, limit)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()

	var out []domain.Transfer
	for rows.Next() {
		t, err := scanTransfer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ReapStuck puts transfers left in PROCESSING back to PENDING so another worker
// can finish them. This is how the service recovers unfinished transfers.
func (s *Store) ReapStuck(ctx context.Context, olderThan time.Duration) (int64, error) {
	const q = `
		UPDATE transfer
		SET state = 'PENDING', claimed_at = NULL, worker_id = NULL
		WHERE state = 'PROCESSING' AND claimed_at < $1`

	tag, err := s.pool.Exec(ctx, q, time.Now().Add(-olderThan))
	if err != nil {
		return 0, classify(err)
	}
	return tag.RowsAffected(), nil
}

// Tx runs fn in a single READ COMMITTED transaction.
func (s *Store) Tx(ctx context.Context, fn func(repository.TxStore) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(&txStore{tx: tx}); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
