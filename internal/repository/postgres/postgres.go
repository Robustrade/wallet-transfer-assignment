package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/model"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/repository"
)

type PostgresRepository struct {
	db *sql.DB
}

var _ repository.Repository = (*PostgresRepository)(nil)

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) Close() error {
	return r.db.Close()
}

func (r *PostgresRepository) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

func (r *PostgresRepository) ExecTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := r.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return fmt.Errorf("tx error: %v, rollback error: %w", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

func (r *PostgresRepository) CreateWallet(ctx context.Context, w *model.Wallet) error {
	query := `
		INSERT INTO wallets (id, balance, currency, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	now := time.Now().UTC()
	if w.CreatedAt.IsZero() {
		w.CreatedAt = now
	}
	if w.UpdatedAt.IsZero() {
		w.UpdatedAt = now
	}

	_, err := r.db.ExecContext(ctx, query, w.ID, w.Balance, w.Currency, w.CreatedAt, w.UpdatedAt)
	return err
}

func (r *PostgresRepository) GetWallet(ctx context.Context, id string) (*model.Wallet, error) {
	query := `
		SELECT id, balance, currency, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`
	var w model.Wallet
	err := r.db.QueryRowContext(ctx, query, id).Scan(&w.ID, &w.Balance, &w.Currency, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrWalletNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// GetWalletsForUpdate acquires row-level locks on both wallets in strict lexicographical order.
// This deterministic locking pattern completely prevents cyclic wait deadlocks during concurrent bidirectional transfers.
func (r *PostgresRepository) GetWalletsForUpdate(ctx context.Context, tx *sql.Tx, id1, id2 string) (*model.Wallet, *model.Wallet, error) {
	if id1 == id2 {
		return nil, nil, model.ErrSelfTransfer
	}

	firstID, secondID := id1, id2
	swapped := false
	if firstID > secondID {
		firstID, secondID = id2, id1
		swapped = true
	}

	lockQuery := `
		SELECT id, balance, currency, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`

	var wFirst model.Wallet
	err := tx.QueryRowContext(ctx, lockQuery, firstID).Scan(
		&wFirst.ID, &wFirst.Balance, &wFirst.Currency, &wFirst.CreatedAt, &wFirst.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, model.ErrWalletNotFound
	}
	if err != nil {
		return nil, nil, err
	}

	var wSecond model.Wallet
	err = tx.QueryRowContext(ctx, lockQuery, secondID).Scan(
		&wSecond.ID, &wSecond.Balance, &wSecond.Currency, &wSecond.CreatedAt, &wSecond.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, model.ErrWalletNotFound
	}
	if err != nil {
		return nil, nil, err
	}

	if swapped {
		return &wSecond, &wFirst, nil
	}
	return &wFirst, &wSecond, nil
}

func (r *PostgresRepository) UpdateWalletBalance(ctx context.Context, tx *sql.Tx, id string, newBalance int64) error {
	query := `
		UPDATE wallets
		SET balance = $1, updated_at = $2
		WHERE id = $3
	`
	res, err := tx.ExecContext(ctx, query, newBalance, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return model.ErrWalletNotFound
	}
	return nil
}

func (r *PostgresRepository) CreateTransfer(ctx context.Context, tx *sql.Tx, t *model.Transfer) error {
	query := `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}

	_, err := tx.ExecContext(ctx, query,
		t.ID, t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount, t.Status, t.FailureReason, t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (r *PostgresRepository) GetTransfer(ctx context.Context, id string) (*model.Transfer, error) {
	query := `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers
		WHERE id = $1
	`
	var t model.Transfer
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status, &t.FailureReason, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrTransferNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *PostgresRepository) GetTransferByIdempotencyKey(ctx context.Context, key string) (*model.Transfer, error) {
	query := `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers
		WHERE idempotency_key = $1
	`
	var t model.Transfer
	err := r.db.QueryRowContext(ctx, query, key).Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status, &t.FailureReason, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrTransferNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *PostgresRepository) CreateLedgerEntries(ctx context.Context, tx *sql.Tx, entries []model.LedgerEntry) error {
	query := `
		INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, entry := range entries {
		now := entry.CreatedAt
		if now.IsZero() {
			now = time.Now().UTC()
		}
		if _, err := stmt.ExecContext(ctx, entry.ID, entry.TransferID, entry.WalletID, entry.EntryType, entry.Amount, now); err != nil {
			return err
		}
	}
	return nil
}

func (r *PostgresRepository) GetLedgerEntriesByTransferID(ctx context.Context, transferID string) ([]model.LedgerEntry, error) {
	query := `
		SELECT id, transfer_id, wallet_id, entry_type, amount, created_at
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query, transferID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []model.LedgerEntry
	for rows.Next() {
		var e model.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.EntryType, &e.Amount, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (r *PostgresRepository) GetLedgerEntriesByWalletID(ctx context.Context, walletID string) ([]model.LedgerEntry, error) {
	query := `
		SELECT id, transfer_id, wallet_id, entry_type, amount, created_at
		FROM ledger_entries
		WHERE wallet_id = $1
		ORDER BY created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query, walletID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []model.LedgerEntry
	for rows.Next() {
		var e model.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.EntryType, &e.Amount, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// GetLedgerBalance reconciles the balance by calculating total credits minus total debits from append-only ledger entries.
func (r *PostgresRepository) GetLedgerBalance(ctx context.Context, walletID string) (int64, error) {
	query := `
		SELECT COALESCE(SUM(
			CASE 
				WHEN entry_type = 'CREDIT' THEN amount
				WHEN entry_type = 'DEBIT' THEN -amount
				ELSE 0
			END
		), 0)
		FROM ledger_entries
		WHERE wallet_id = $1
	`
	var balance int64
	err := r.db.QueryRowContext(ctx, query, walletID).Scan(&balance)
	if err != nil {
		return 0, err
	}
	return balance, nil
}

func (r *PostgresRepository) GetIdempotencyRecord(ctx context.Context, key string) (*model.IdempotencyRecord, error) {
	query := `
		SELECT key, request_hash, status, response_status, response_body, transfer_id, created_at, updated_at
		FROM idempotency_records
		WHERE key = $1
	`
	var rec model.IdempotencyRecord
	err := r.db.QueryRowContext(ctx, query, key).Scan(
		&rec.Key, &rec.RequestHash, &rec.Status, &rec.ResponseStatus, &rec.ResponseBody, &rec.TransferID, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // Return nil when not found
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *PostgresRepository) SaveIdempotencyRecord(ctx context.Context, tx *sql.Tx, rec *model.IdempotencyRecord) error {
	query := `
		INSERT INTO idempotency_records (key, request_hash, status, response_status, response_body, transfer_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (key) DO UPDATE SET
			status = EXCLUDED.status,
			response_status = EXCLUDED.response_status,
			response_body = EXCLUDED.response_body,
			transfer_id = EXCLUDED.transfer_id,
			updated_at = EXCLUDED.updated_at
	`
	now := time.Now().UTC()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now

	_, err := tx.ExecContext(ctx, query,
		rec.Key, rec.RequestHash, rec.Status, rec.ResponseStatus, rec.ResponseBody, rec.TransferID, rec.CreatedAt, rec.UpdatedAt,
	)
	return err
}

// IsUniqueViolation checks if an error represents a PostgreSQL unique constraint violation (code 23505).
func IsUniqueViolation(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == "23505"
	}
	return false
}
