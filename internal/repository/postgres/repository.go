package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer/internal/domain"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{
		db: db,
	}
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.db.Ping(ctx)
}

func (r *Repository) GetWalletForUpdate(
	ctx context.Context,
	tx pgx.Tx,
	walletID uuid.UUID,
) (*domain.Wallet, error) {

	query := `
		SELECT id, balance, created_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`

	var wallet domain.Wallet

	err := tx.QueryRow(ctx, query, walletID).Scan(
		&wallet.ID,
		&wallet.Balance,
		&wallet.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("wallet not found: %s", walletID)
		}

		return nil, fmt.Errorf("get wallet for update: %w", err)
	}

	return &wallet, nil
}
func (r *Repository) UpdateWalletBalance(
	ctx context.Context,
	tx pgx.Tx,
	walletID uuid.UUID,
	newBalance int64,
) error {

	query := `
		UPDATE wallets
		SET balance = $1
		WHERE id = $2
	`

	commandTag, err := tx.Exec(
		ctx,
		query,
		newBalance,
		walletID,
	)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}

	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("wallet not found: %s", walletID)
	}

	return nil
}
func (r *Repository) CreateTransfer(
	ctx context.Context,
	tx pgx.Tx,
	transfer *domain.Transfer,
) error {

	query := `
		INSERT INTO transfers (
			id,
			from_wallet_id,
			to_wallet_id,
			amount,
			status,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := tx.Exec(
		ctx,
		query,
		transfer.ID,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		transfer.Status,
		transfer.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create transfer: %w", err)
	}

	return nil
}
func (r *Repository) CreateLedgerEntry(
	ctx context.Context,
	tx pgx.Tx,
	entry *domain.LedgerEntry,
) error {

	query := `
		INSERT INTO ledger_entries (
			id,
			transfer_id,
			wallet_id,
			entry_type,
			amount,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := tx.Exec(
		ctx,
		query,
		entry.ID,
		entry.TransferID,
		entry.WalletID,
		entry.EntryType,
		entry.Amount,
		entry.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create ledger entry: %w", err)
	}

	return nil
}
func (r *Repository) UpdateTransferStatus(
	ctx context.Context,
	tx pgx.Tx,
	transferID uuid.UUID,
	status domain.TransferStatus,
) error {

	query := `
		UPDATE transfers
		SET status = $1
		WHERE id = $2
		  AND status = 'PENDING'
	`

	commandTag, err := tx.Exec(
		ctx,
		query,
		status,
		transferID,
	)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}

	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("transfer not found or not in PENDING state: %s", transferID)
	}

	return nil
}
func (r *Repository) CreateIdempotencyRecord(
	ctx context.Context,
	tx pgx.Tx,
	idempotencyKey string,
	transferID uuid.UUID,
) error {

	query := `
		INSERT INTO idempotency_records (
			idempotency_key,
			transfer_id
		)
		VALUES ($1, $2)
	`

	_, err := tx.Exec(
		ctx,
		query,
		idempotencyKey,
		transferID,
	)
	if err != nil {
		return fmt.Errorf("create idempotency record: %w", err)
	}

	return nil
}
func (r *Repository) GetTransferByIdempotencyKey(
	ctx context.Context,
	idempotencyKey string,
) (*domain.Transfer, error) {

	query := `
		SELECT
			t.id,
			t.from_wallet_id,
			t.to_wallet_id,
			t.amount,
			t.status,
			t.created_at
		FROM idempotency_records i
		JOIN transfers t
			ON t.id = i.transfer_id
		WHERE i.idempotency_key = $1
	`

	var transfer domain.Transfer

	err := r.db.QueryRow(
		ctx,
		query,
		idempotencyKey,
	).Scan(
		&transfer.ID,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&transfer.Status,
		&transfer.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, fmt.Errorf(
			"get transfer by idempotency key: %w",
			err,
		)
	}

	return &transfer, nil
}
