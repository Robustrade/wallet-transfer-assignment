package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/synchone/wallet-transfer-assignment/internal/domain"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *Repository) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return r.pool.Begin(ctx)
}

func (r *Repository) FindTransferForUpdate(ctx context.Context, tx pgx.Tx, key string) (*domain.Transfer, error) {
	const q = `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, response_snapshot, created_at, updated_at
		FROM transfers
		WHERE idempotency_key = $1
		FOR UPDATE
	`
	row := tx.QueryRow(ctx, q, key)
	transfer, err := scanTransfer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrTransferNotFound
	}
	return transfer, err
}

func (r *Repository) CreateTransfer(ctx context.Context, tx pgx.Tx, input domain.CreateTransferInput) (*domain.Transfer, error) {
	const q = `
		INSERT INTO transfers (idempotency_key, from_wallet_id, to_wallet_id, amount, status)
		VALUES ($1, $2, $3, $4, 'PENDING')
		RETURNING id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, response_snapshot, created_at, updated_at
	`
	row := tx.QueryRow(ctx, q, input.IdempotencyKey, input.FromWalletID, input.ToWalletID, input.Amount)
	return scanTransfer(row)
}

func (r *Repository) LockWallets(ctx context.Context, tx pgx.Tx, walletIDs ...string) (map[string]domain.Wallet, error) {
	const q = `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = ANY($1)
		ORDER BY id
		FOR UPDATE
	`
	rows, err := tx.Query(ctx, q, walletIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	wallets := make(map[string]domain.Wallet, len(walletIDs))
	for rows.Next() {
		var w domain.Wallet
		if err := rows.Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		wallets[w.ID] = w
	}
	return wallets, rows.Err()
}

func (r *Repository) UpdateWalletBalance(ctx context.Context, tx pgx.Tx, walletID string, delta int64) error {
	const q = `
		UPDATE wallets
		SET balance = balance + $2, updated_at = now()
		WHERE id = $1
	`
	_, err := tx.Exec(ctx, q, walletID, delta)
	return err
}

func (r *Repository) InsertLedgerEntry(ctx context.Context, tx pgx.Tx, transferID uuid.UUID, walletID string, entryType domain.LedgerEntryType, amount int64) (int64, error) {
	const q = `
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`
	var id int64
	err := tx.QueryRow(ctx, q, transferID, walletID, entryType, amount).Scan(&id)
	return id, err
}

func (r *Repository) ListLedgerEntries(ctx context.Context, tx pgx.Tx, transferID uuid.UUID) ([]domain.LedgerEntry, error) {
	const q = `
		SELECT id, transfer_id, wallet_id, entry_type, amount, created_at
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY id
	`
	rows, err := tx.Query(ctx, q, transferID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.EntryType, &e.Amount, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (r *Repository) UpdateTransferStatus(ctx context.Context, tx pgx.Tx, transferID uuid.UUID, status domain.TransferStatus, failureReason *string) error {
	const q = `
		UPDATE transfers
		SET status = $2, failure_reason = $3, updated_at = now()
		WHERE id = $1
	`
	_, err := tx.Exec(ctx, q, transferID, status, failureReason)
	return err
}

func (r *Repository) StoreResponseSnapshot(ctx context.Context, tx pgx.Tx, transferID uuid.UUID, snapshot json.RawMessage) error {
	const q = `
		UPDATE transfers
		SET response_snapshot = $2, updated_at = now()
		WHERE id = $1
	`
	_, err := tx.Exec(ctx, q, transferID, snapshot)
	return err
}

func (r *Repository) GetWalletBalance(ctx context.Context, walletID string) (int64, error) {
	const q = `SELECT balance FROM wallets WHERE id = $1`
	var balance int64
	err := r.pool.QueryRow(ctx, q, walletID).Scan(&balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrWalletNotFound
	}
	return balance, err
}

func scanTransfer(row pgx.Row) (*domain.Transfer, error) {
	var t domain.Transfer
	err := row.Scan(
		&t.ID,
		&t.IdempotencyKey,
		&t.FromWalletID,
		&t.ToWalletID,
		&t.Amount,
		&t.Status,
		&t.FailureReason,
		&t.ResponseSnapshot,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func IsCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}

func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}
