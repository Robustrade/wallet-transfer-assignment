package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/malem-mayeng/wallet-transfer-assignment/internal/domain"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

type postgresTransferStore struct {
	tx pgx.Tx
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{
		pool: pool,
	}
}

func (r *PostgresRepository) GetTransferByIdempotencyKey(
	ctx context.Context,
	idempotencyKey string,
) (*domain.Transfer, error) {
	const query = `
		SELECT
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status,
			created_at,
			updated_at
		FROM transfers
		WHERE idempotency_key = $1
	`

	var transfer domain.Transfer

	err := r.pool.QueryRow(
		ctx,
		query,
		idempotencyKey,
	).Scan(
		&transfer.ID,
		&transfer.IdempotencyKey,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&transfer.Status,
		&transfer.CreatedAt,
		&transfer.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrTransferNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get transfer by idempotency key: %w", err)
	}

	return &transfer, nil
}

func (r *PostgresRepository) WithTx(
	ctx context.Context,
	fn func(context.Context, TransferStore) error,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	store := &postgresTransferStore{
		tx: tx,
	}

	if err := fn(ctx, store); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (r *postgresTransferStore) CreateOrGetTransfer(
	ctx context.Context,
	transfer *domain.Transfer,
) (*domain.Transfer, bool, error) {
	const insertQuery = `
		INSERT INTO transfers (
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status
		)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status,
			created_at,
			updated_at
	`

	var createdTransfer domain.Transfer

	err := r.tx.QueryRow(
		ctx,
		insertQuery,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		transfer.Status,
	).Scan(
		&createdTransfer.ID,
		&createdTransfer.IdempotencyKey,
		&createdTransfer.FromWalletID,
		&createdTransfer.ToWalletID,
		&createdTransfer.Amount,
		&createdTransfer.Status,
		&createdTransfer.CreatedAt,
		&createdTransfer.UpdatedAt,
	)

	if err == nil {
		return &createdTransfer, true, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("create transfer: %w", err)
	}

	const selectQuery = `
		SELECT
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status,
			created_at,
			updated_at
		FROM transfers
		WHERE idempotency_key = $1
	`

	err = r.tx.QueryRow(
		ctx,
		selectQuery,
		transfer.IdempotencyKey,
	).Scan(
		&createdTransfer.ID,
		&createdTransfer.IdempotencyKey,
		&createdTransfer.FromWalletID,
		&createdTransfer.ToWalletID,
		&createdTransfer.Amount,
		&createdTransfer.Status,
		&createdTransfer.CreatedAt,
		&createdTransfer.UpdatedAt,
	)

	if err != nil {
		return nil, false, fmt.Errorf("get existing transfer: %w", err)
	}

	return &createdTransfer, false, nil
}

func (r *postgresTransferStore) GetWalletForUpdate(
	ctx context.Context,
	walletID string,
) (*domain.Wallet, error) {
	const query = `
		SELECT
			id,
			balance,
			created_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`

	var wallet domain.Wallet

	err := r.tx.QueryRow(
		ctx,
		query,
		walletID,
	).Scan(
		&wallet.ID,
		&wallet.Balance,
		&wallet.CreatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrWalletNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get wallet for update: %w", err)
	}

	return &wallet, nil
}

func (r *postgresTransferStore) UpdateWalletBalance(
	ctx context.Context,
	walletID string,
	balance int64,
) error {
	const query = `
		UPDATE wallets
		SET balance = $1
		WHERE id = $2
	`

	result, err := r.tx.Exec(ctx, query, balance, walletID)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}

	if result.RowsAffected() != 1 {
		return domain.ErrWalletNotFound
	}

	return nil
}

func (r *postgresTransferStore) CreateLedgerEntry(
	ctx context.Context,
	entry *domain.LedgerEntry,
) error {
	const query = `
		INSERT INTO ledger_entries (
			transfer_id,
			wallet_id,
			entry_type,
			amount
		)
		VALUES ($1, $2, $3, $4)
	`

	_, err := r.tx.Exec(
		ctx,
		query,
		entry.TransferID,
		entry.WalletID,
		entry.EntryType,
		entry.Amount,
	)

	if err != nil {
		return fmt.Errorf("create ledger entry: %w", err)
	}

	return nil
}

func (r *postgresTransferStore) UpdateTransferStatus(
	ctx context.Context,
	transferID int64,
	status domain.TransferStatus,
) error {
	const query = `
		UPDATE transfers
		SET
			status = $1,
			updated_at = NOW()
		WHERE id = $2
		AND status = 'PENDING'
	`

	result, err := r.tx.Exec(ctx, query, status, transferID)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}

	if result.RowsAffected() != 1 {
		return domain.ErrTransferNotFound
	}

	return nil
}
