package repositories

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/parth/wallet-transfer-assignment/internal/domain"
)

type TransferRepository struct {
	db *pgxpool.Pool
}

func NewTransferRepository(db *pgxpool.Pool) *TransferRepository {
	return &TransferRepository{db: db}
}

// GetWalletByID retrieves a wallet by ID
func (r *TransferRepository) GetWalletByID(ctx context.Context, walletID string) (*domain.Wallet, error) {
	wallet := &domain.Wallet{}
	err := r.db.QueryRow(ctx, `
		SELECT wallet_id, balance, created_at, updated_at
		FROM wallets
		WHERE wallet_id = $1
	`, walletID).Scan(&wallet.WalletID, &wallet.Balance, &wallet.CreatedAt, &wallet.UpdatedAt)

	if err == pgx.ErrNoRows {
		return nil, errors.New("wallet not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch wallet: %w", err)
	}
	return wallet, nil
}

// GetWalletForUpdate retrieves a wallet with a row-level lock for update
func (r *TransferRepository) GetWalletForUpdate(ctx context.Context, tx pgx.Tx, walletID string) (*domain.Wallet, error) {
	wallet := &domain.Wallet{}
	err := tx.QueryRow(ctx, `
		SELECT wallet_id, balance, created_at, updated_at
		FROM wallets
		WHERE wallet_id = $1
		FOR UPDATE
	`, walletID).Scan(&wallet.WalletID, &wallet.Balance, &wallet.CreatedAt, &wallet.UpdatedAt)

	if err == pgx.ErrNoRows {
		return nil, errors.New("wallet not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch wallet for update: %w", err)
	}
	return wallet, nil
}

// InsertTransfer creates a new transfer record
func (r *TransferRepository) InsertTransfer(ctx context.Context, tx pgx.Tx, transfer *domain.Transfer) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO transfers (transfer_id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
	`, transfer.TransferID, transfer.IdempotencyKey, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount, transfer.State)

	if err != nil {
		return fmt.Errorf("failed to insert transfer: %w", err)
	}
	return nil
}

// UpdateTransferState updates the state of a transfer
func (r *TransferRepository) UpdateTransferState(ctx context.Context, tx pgx.Tx, transferID, newState string) error {
	_, err := tx.Exec(ctx, `
		UPDATE transfers
		SET state = $1, updated_at = NOW()
		WHERE transfer_id = $2
	`, newState, transferID)

	if err != nil {
		return fmt.Errorf("failed to update transfer state: %w", err)
	}
	return nil
}

// InsertLedgerEntry creates a ledger entry (debit or credit)
func (r *TransferRepository) InsertLedgerEntry(ctx context.Context, tx pgx.Tx, entry *domain.LedgerEntry) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO ledger_entries (entry_id, wallet_id, transfer_id, type, amount, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`, entry.EntryID, entry.WalletID, entry.TransferID, entry.Type, entry.Amount)

	if err != nil {
		return fmt.Errorf("failed to insert ledger entry: %w", err)
	}
	return nil
}

// UpdateWalletBalance updates wallet balance
func (r *TransferRepository) UpdateWalletBalance(ctx context.Context, tx pgx.Tx, walletID string, newBalance string) error {
	_, err := tx.Exec(ctx, `
		UPDATE wallets
		SET balance = $1, updated_at = NOW()
		WHERE wallet_id = $2
	`, newBalance, walletID)

	if err != nil {
		return fmt.Errorf("failed to update wallet balance: %w", err)
	}
	return nil
}

// GetIdempotencyRecord retrieves an idempotency record if it exists
func (r *TransferRepository) GetIdempotencyRecord(ctx context.Context, idempotencyKey string) (*domain.IdempotencyRecord, error) {
	record := &domain.IdempotencyRecord{}
	err := r.db.QueryRow(ctx, `
		SELECT idempotency_key, transfer_id, response_payload, created_at
		FROM idempotency_records
		WHERE idempotency_key = $1
	`, idempotencyKey).Scan(&record.IdempotencyKey, &record.TransferID, &record.ResponsePayload, &record.CreatedAt)

	if err == pgx.ErrNoRows {
		return nil, nil // Not found, but no error
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch idempotency record: %w", err)
	}
	return record, nil
}

// InsertIdempotencyRecord creates an idempotency record
func (r *TransferRepository) InsertIdempotencyRecord(ctx context.Context, tx pgx.Tx, record *domain.IdempotencyRecord) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO idempotency_records (idempotency_key, transfer_id, response_payload, created_at)
		VALUES ($1, $2, $3, NOW())
	`, record.IdempotencyKey, record.TransferID, record.ResponsePayload)

	if err != nil {
		return fmt.Errorf("failed to insert idempotency record: %w", err)
	}
	return nil
}

// GetTransferByID retrieves a transfer by its ID
func (r *TransferRepository) GetTransferByID(ctx context.Context, transferID string) (*domain.Transfer, error) {
	transfer := &domain.Transfer{}
	err := r.db.QueryRow(ctx, `
		SELECT transfer_id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, created_at, updated_at
		FROM transfers
		WHERE transfer_id = $1
	`, transferID).Scan(&transfer.TransferID, &transfer.IdempotencyKey, &transfer.FromWalletID, &transfer.ToWalletID, &transfer.Amount, &transfer.State, &transfer.CreatedAt, &transfer.UpdatedAt)

	if err == pgx.ErrNoRows {
		return nil, domain.ErrTransferNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch transfer: %w", err)
	}
	return transfer, nil
}

// GetTransferByIdempotencyKey retrieves a transfer by its idempotency key
func (r *TransferRepository) GetTransferByIdempotencyKey(ctx context.Context, idempotencyKey string) (*domain.Transfer, error) {
	transfer := &domain.Transfer{}
	err := r.db.QueryRow(ctx, `
		SELECT transfer_id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, created_at, updated_at
		FROM transfers
		WHERE idempotency_key = $1
	`, idempotencyKey).Scan(&transfer.TransferID, &transfer.IdempotencyKey, &transfer.FromWalletID, &transfer.ToWalletID, &transfer.Amount, &transfer.State, &transfer.CreatedAt, &transfer.UpdatedAt)

	if err == pgx.ErrNoRows {
		return nil, nil // Not found, but no error
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch transfer by idempotency key: %w", err)
	}
	return transfer, nil
}
