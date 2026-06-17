package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Loads and row-locks a wallet.
func (r *pgRepository) GetWalletForUpdate(ctx context.Context, walletID string) (*domain.Wallet, error) {
	const q = `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE`

	var w domain.Wallet
	err := r.tx.QueryRow(ctx, q, walletID).
		Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get wallet for update: %w", err)
	}
	return &w, nil
}

// Sets a wallet balance.
func (r *pgRepository) UpdateWalletBalance(ctx context.Context, walletID string, newBalance int64) error {
	const q = `
		UPDATE wallets
		SET balance = $2, updated_at = now()
		WHERE id = $1`

	ct, err := r.tx.Exec(ctx, q, walletID, newBalance)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrWalletNotFound
	}
	return nil
}

// Inserts a transfer; DB generates id and timestamps.
func (r *pgRepository) CreateTransfer(ctx context.Context, t *domain.Transfer) (*domain.Transfer, error) {
	const q = `
		INSERT INTO transfers (from_wallet_id, to_wallet_id, amount, status)
		VALUES ($1, $2, $3, $4)
		RETURNING id, status, created_at, updated_at`

	out := *t
	err := r.tx.QueryRow(ctx, q, t.FromWalletID, t.ToWalletID, t.Amount, string(t.Status)).
		Scan(&out.ID, &out.Status, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create transfer: %w", err)
	}
	return &out, nil
}

// Updates a transfer status.
func (r *pgRepository) UpdateTransferStatus(ctx context.Context, transferID string, status domain.TransferStatus) error {
	const q = `
		UPDATE transfers
		SET status = $2, updated_at = now()
		WHERE id = $1`

	ct, err := r.tx.Exec(ctx, q, transferID, string(status))
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrTransferNotFound
	}
	return nil
}

// Loads a transfer by id.
func (r *pgRepository) GetTransferByID(ctx context.Context, transferID string) (*domain.Transfer, error) {
	const q = `
		SELECT id, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at
		FROM transfers
		WHERE id = $1`

	var t domain.Transfer
	var status string
	err := r.tx.QueryRow(ctx, q, transferID).
		Scan(&t.ID, &t.FromWalletID, &t.ToWalletID, &t.Amount, &status, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTransferNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get transfer: %w", err)
	}
	t.Status = domain.TransferStatus(status)
	return &t, nil
}

// Inserts one ledger entry.
func (r *pgRepository) CreateLedgerEntry(ctx context.Context, e *domain.LedgerEntry) error {
	const q = `
		INSERT INTO ledger_entries (wallet_id, transfer_id, type, amount)
		VALUES ($1, $2, $3, $4)`

	_, err := r.tx.Exec(ctx, q, e.WalletID, e.TransferID, string(e.Type), e.Amount)
	if err != nil {
		return fmt.Errorf("create ledger entry: %w", err)
	}
	return nil
}

// Links an idempotency key to a transfer. Maps unique violation to a sentinel.
func (r *pgRepository) CreateIdempotencyRecord(ctx context.Context, key string, transferID string) error {
	const q = `
		INSERT INTO idempotency_records (idempotency_key, transfer_id)
		VALUES ($1, $2)`

	_, err := r.tx.Exec(ctx, q, key, transferID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDuplicateIdempotencyKey
		}
		return fmt.Errorf("create idempotency record: %w", err)
	}
	return nil
}

// Resolves an idempotency key to its transfer id.
func (r *pgRepository) GetTransferIDByIdempotencyKey(ctx context.Context, key string) (string, error) {
	const q = `
		SELECT transfer_id
		FROM idempotency_records
		WHERE idempotency_key = $1`

	var transferID string
	err := r.tx.QueryRow(ctx, q, key).Scan(&transferID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrIdempotencyKeyNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get transfer id by key: %w", err)
	}
	return transferID, nil
}