package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-service/internal/domain"
)

type TransferRepository interface {
	BeginTx(
		ctx context.Context,
	) (pgx.Tx, error)

	// Idempotency
	GetTransferByIdempotencyKey(
		ctx context.Context,
		tx pgx.Tx,
		key string,
	) (*domain.Transfer, error)

	CreateTransfer(
		ctx context.Context,
		tx pgx.Tx,
		transfer *domain.Transfer,
	) error

	// Wallet
	LockWallet(
		ctx context.Context,
		tx pgx.Tx,
		walletID string,
	) (*domain.Wallet, error)

	UpdateWalletBalance(
		ctx context.Context,
		tx pgx.Tx,
		walletID string,
		balance int64,
	) error

	// Ledger
	CreateLedgerEntry(
		ctx context.Context,
		tx pgx.Tx,
		entry *domain.LedgerEntry,
	) error

	// Transfer status
	UpdateTransferStatus(
		ctx context.Context,
		tx pgx.Tx,
		transferID string,
		status domain.TransferStatus,
		response []byte,
	) error
}

// ============================================================
// PostgreSQL implementation
// ============================================================

type PostgresTransferRepository struct {
	db *pgxpool.Pool
}

func NewPostgresTransferRepository(
	db *pgxpool.Pool,
) *PostgresTransferRepository {

	return &PostgresTransferRepository{
		db: db,
	}
}

// ============================================================
// Get transfer by idempotency key
// ============================================================

func (r *PostgresTransferRepository) GetTransferByIdempotencyKey(
	ctx context.Context,
	tx pgx.Tx,
	key string,
) (*domain.Transfer, error) {

	var transfer domain.Transfer

	err := tx.QueryRow(
		ctx,
		`
		SELECT
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status,
			response,
			created_at,
			updated_at
		FROM transfers
		WHERE idempotency_key = $1
		`,
		key,
	).Scan(
		&transfer.ID,
		&transfer.IdempotencyKey,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&transfer.Status,
		&transfer.Response,
		&transfer.CreatedAt,
		&transfer.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &transfer, nil
}

// ============================================================
// Create transfer
// ============================================================

func (r *PostgresTransferRepository) CreateTransfer(
	ctx context.Context,
	tx pgx.Tx,
	transfer *domain.Transfer,
) error {

	_, err := tx.Exec(
		ctx,
		`
		INSERT INTO transfers (
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status,
			response
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		`,
		transfer.ID,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		transfer.Status,
		transfer.Response,
	)

	return err
}

// ============================================================
// Lock wallet
// ============================================================

func (r *PostgresTransferRepository) LockWallet(
	ctx context.Context,
	tx pgx.Tx,
	walletID string,
) (*domain.Wallet, error) {

	var wallet domain.Wallet

	err := tx.QueryRow(
		ctx,
		`
		SELECT
			id,
			balance
		FROM wallets
		WHERE id = $1
		FOR UPDATE
		`,
		walletID,
	).Scan(
		&wallet.ID,
		&wallet.Balance,
	)

	if err != nil {
		return nil, err
	}

	return &wallet, nil
}

// ============================================================
// Update wallet balance
// ============================================================

func (r *PostgresTransferRepository) UpdateWalletBalance(
	ctx context.Context,
	tx pgx.Tx,
	walletID string,
	balance int64,
) error {

	_, err := tx.Exec(
		ctx,
		`
		UPDATE wallets
		SET balance = $1
		WHERE id = $2
		`,
		balance,
		walletID,
	)

	return err
}

// ============================================================
// Create ledger entry
// ============================================================

func (r *PostgresTransferRepository) CreateLedgerEntry(
	ctx context.Context,
	tx pgx.Tx,
	entry *domain.LedgerEntry,
) error {

	_, err := tx.Exec(
		ctx,
		`
		INSERT INTO ledger_entries (
			wallet_id,
			transfer_id,
			type,
			amount
		)
		VALUES ($1, $2, $3, $4)
		`,
		entry.WalletID,
		entry.TransferID,
		entry.Type,
		entry.Amount,
	)

	return err
}

// ============================================================
// Update transfer status
// ============================================================

func (r *PostgresTransferRepository) UpdateTransferStatus(
	ctx context.Context,
	tx pgx.Tx,
	transferID string,
	status domain.TransferStatus,
	response []byte,
) error {

	_, err := tx.Exec(
		ctx,
		`
		UPDATE transfers
		SET
			status = $1,
			response = $2,
			updated_at = NOW()
		WHERE id = $3
		`,
		status,
		response,
		transferID,
	)

	return err
}
func (r *PostgresTransferRepository) BeginTx(
	ctx context.Context,
) (pgx.Tx, error) {

	return r.db.Begin(ctx)
}

// Compile-time interface check.
var _ TransferRepository = (*PostgresTransferRepository)(nil)
