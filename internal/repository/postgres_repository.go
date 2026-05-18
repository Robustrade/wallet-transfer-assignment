package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"wallet-transfer-service/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("record not found")

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{
		pool: pool,
	}
}

func (s *PostgresStore) WithTx(
	ctx context.Context,
	fn func(ctx context.Context, repo Repository) error,
) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	repo := &PostgresRepository{
		tx: tx,
	}

	if err := fn(ctx, repo); err != nil {
		rollbackErr := tx.Rollback(ctx)
		if rollbackErr != nil {
			return fmt.Errorf("tx failed: %w, rollback failed: %v", err, rollbackErr)
		}

		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

type PostgresRepository struct {
	tx pgx.Tx
}

func (r *PostgresRepository) GetIdempotencyRecordForUpdate(
	ctx context.Context,
	idempotencyKey string,
) (*IdempotencyRecord, error) {
	const query = `
		SELECT
			idempotency_key,
			request_hash,
			transfer_id,
			response_code,
			response_body,
			status
		FROM idempotency_records
		WHERE idempotency_key = $1
		FOR UPDATE;
	`

	var record IdempotencyRecord

	err := r.tx.QueryRow(ctx, query, idempotencyKey).Scan(
		&record.IdempotencyKey,
		&record.RequestHash,
		&record.TransferID,
		&record.ResponseCode,
		&record.ResponseBody,
		&record.Status,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}

		return nil, fmt.Errorf("get idempotency record for update: %w", err)
	}

	return &record, nil
}

func (r *PostgresRepository) TryCreateIdempotencyRecord(
	ctx context.Context,
	idempotencyKey string,
	requestHash string,
) (bool, error) {
	const query = `
		INSERT INTO idempotency_records (
			idempotency_key,
			request_hash,
			status
		)
		VALUES ($1, $2, 'STARTED')
		ON CONFLICT (idempotency_key) DO NOTHING;
	`

	result, err := r.tx.Exec(ctx, query, idempotencyKey, requestHash)
	if err != nil {
		return false, fmt.Errorf("try create idempotency record: %w", err)
	}

	return result.RowsAffected() == 1, nil
}

func (r *PostgresRepository) CompleteIdempotencyRecord(
	ctx context.Context,
	idempotencyKey string,
	transferID string,
	responseCode int,
	responseBody []byte,
) error {
	const query = `
		UPDATE idempotency_records
		SET
			transfer_id = $2,
			response_code = $3,
			response_body = $4,
			status = 'COMPLETED',
			updated_at = NOW()
		WHERE idempotency_key = $1;
	`

	result, err := r.tx.Exec(
		ctx,
		query,
		idempotencyKey,
		transferID,
		responseCode,
		responseBody,
	)
	if err != nil {
		return fmt.Errorf("complete idempotency record: %w", err)
	}

	if result.RowsAffected() != 1 {
		return ErrNotFound
	}

	return nil
}

func (r *PostgresRepository) FailIdempotencyRecord(
	ctx context.Context,
	idempotencyKey string,
	transferID string,
	responseCode int,
	responseBody []byte,
) error {
	const query = `
		UPDATE idempotency_records
		SET
			transfer_id = $2,
			response_code = $3,
			response_body = $4,
			status = 'FAILED',
			updated_at = NOW()
		WHERE idempotency_key = $1;
	`

	result, err := r.tx.Exec(
		ctx,
		query,
		idempotencyKey,
		transferID,
		responseCode,
		responseBody,
	)
	if err != nil {
		return fmt.Errorf("fail idempotency record: %w", err)
	}

	if result.RowsAffected() != 1 {
		return ErrNotFound
	}

	return nil
}

func (r *PostgresRepository) CreateTransfer(
	ctx context.Context,
	transfer *domain.Transfer,
) error {
	const query = `
		INSERT INTO transfers (
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status,
			failure_reason
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7);
	`

	_, err := r.tx.Exec(
		ctx,
		query,
		transfer.ID,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		transfer.Status,
		transfer.FailureReason,
	)
	if err != nil {
		return fmt.Errorf("create transfer: %w", err)
	}

	return nil
}

func (r *PostgresRepository) UpdateTransferStatus(
	ctx context.Context,
	transferID string,
	status domain.TransferStatus,
	failureReason *string,
) error {
	const query = `
		UPDATE transfers
		SET
			status = $2,
			failure_reason = $3,
			updated_at = NOW()
		WHERE id = $1
		  AND status = 'PENDING';
	`

	result, err := r.tx.Exec(ctx, query, transferID, status, failureReason)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}

	if result.RowsAffected() != 1 {
		return domain.ErrInvalidTransferState
	}

	return nil
}

func (r *PostgresRepository) LockWalletsForUpdate(
	ctx context.Context,
	walletIDs []string,
) (map[string]*domain.Wallet, error) {
	if len(walletIDs) == 0 {
		return map[string]*domain.Wallet{}, nil
	}

	sortedIDs := append([]string(nil), walletIDs...)
	sort.Strings(sortedIDs)

	const query = `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = ANY($1)
		ORDER BY id
		FOR UPDATE;
	`

	rows, err := r.tx.Query(ctx, query, sortedIDs)
	if err != nil {
		return nil, fmt.Errorf("lock wallets for update: %w", err)
	}
	defer rows.Close()

	wallets := make(map[string]*domain.Wallet, len(sortedIDs))

	for rows.Next() {
		var wallet domain.Wallet

		if err := rows.Scan(
			&wallet.ID,
			&wallet.Balance,
			&wallet.CreatedAt,
			&wallet.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan wallet: %w", err)
		}

		wallets[wallet.ID] = &wallet
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("wallet rows error: %w", err)
	}

	if len(wallets) != len(sortedIDs) {
		return nil, ErrNotFound
	}

	return wallets, nil
}

func (r *PostgresRepository) UpdateWalletBalance(
	ctx context.Context,
	walletID string,
	balance int64,
) error {
	const query = `
		UPDATE wallets
		SET
			balance = $2,
			updated_at = NOW()
		WHERE id = $1;
	`

	result, err := r.tx.Exec(ctx, query, walletID, balance)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}

	if result.RowsAffected() != 1 {
		return ErrNotFound
	}

	return nil
}

func (r *PostgresRepository) InsertLedgerEntries(
	ctx context.Context,
	entries ...*domain.LedgerEntry,
) error {
	const query = `
		INSERT INTO ledger_entries (
			id,
			wallet_id,
			transfer_id,
			type,
			amount
		)
		VALUES ($1, $2, $3, $4, $5);
	`

	for _, entry := range entries {
		_, err := r.tx.Exec(
			ctx,
			query,
			entry.ID,
			entry.WalletID,
			entry.TransferID,
			entry.Type,
			entry.Amount,
		)
		if err != nil {
			return fmt.Errorf("insert ledger entry: %w", err)
		}
	}

	return nil
}
