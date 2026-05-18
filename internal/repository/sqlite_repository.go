package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/wallet-transfer-assignment/internal/domain"
)

// SQLiteRepository implements Repository for SQLite.
type SQLiteRepository struct {
	db *sql.DB
}

// SQLiteTxRepository is a transaction-scoped repository for SQLite.
type SQLiteTxRepository struct {
	tx *sql.Tx
}

// NewSQLiteRepository creates a new SQLite repository.
func NewSQLiteRepository(db *sql.DB) *SQLiteRepository {
	return &SQLiteRepository{db: db}
}

// --- TxManager ---

// WithTransaction executes fn within a SQLite transaction.
// With MaxOpenConns=1 and WAL mode + busy timeout configured on the connection,
// all write transactions are effectively serialized.
func (r *SQLiteRepository) WithTransaction(ctx context.Context, fn func(repo Repository) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	txRepo := &SQLiteTxRepository{tx: tx}

	if err := fn(txRepo); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("rollback error: %v (original: %w)", rbErr, err)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// --- Wallet operations ---

func (r *SQLiteTxRepository) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	wallet := &domain.Wallet{}
	err := r.tx.QueryRowContext(ctx,
		"SELECT id, balance, created_at, updated_at FROM wallets WHERE id = ?", id,
	).Scan(&wallet.ID, &wallet.Balance, &wallet.CreatedAt, &wallet.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, domain.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}
	return wallet, nil
}

// GetWalletForUpdate in SQLite is identical to GetWallet because
// the IMMEDIATE transaction already holds a write lock.
func (r *SQLiteTxRepository) GetWalletForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	return r.GetWallet(ctx, id)
}

func (r *SQLiteTxRepository) DebitWallet(ctx context.Context, id string, amount int64) error {
	result, err := r.tx.ExecContext(ctx,
		"UPDATE wallets SET balance = balance - ?, updated_at = ? WHERE id = ? AND balance >= ?",
		amount, time.Now().UTC(), id, amount,
	)
	if err != nil {
		return fmt.Errorf("debit wallet: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("debit wallet rows affected: %w", err)
	}
	if rows == 0 {
		return domain.ErrInsufficientBalance
	}
	return nil
}

func (r *SQLiteTxRepository) CreditWallet(ctx context.Context, id string, amount int64) error {
	_, err := r.tx.ExecContext(ctx,
		"UPDATE wallets SET balance = balance + ?, updated_at = ? WHERE id = ?",
		amount, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("credit wallet: %w", err)
	}
	return nil
}

// --- Transfer operations ---

func (r *SQLiteTxRepository) CreateTransfer(ctx context.Context, transfer *domain.Transfer) error {
	_, err := r.tx.ExecContext(ctx,
		`INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, idempotency_key, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		transfer.ID, transfer.FromWalletID, transfer.ToWalletID,
		transfer.Amount, transfer.Status, transfer.IdempotencyKey,
		transfer.CreatedAt, transfer.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create transfer: %w", err)
	}
	return nil
}

func (r *SQLiteTxRepository) UpdateTransferStatus(ctx context.Context, id string, status domain.TransferStatus) error {
	_, err := r.tx.ExecContext(ctx,
		"UPDATE transfers SET status = ?, updated_at = ? WHERE id = ?",
		status, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}
	return nil
}

func (r *SQLiteTxRepository) GetTransfer(ctx context.Context, id string) (*domain.Transfer, error) {
	transfer := &domain.Transfer{}
	err := r.tx.QueryRowContext(ctx,
		`SELECT id, from_wallet_id, to_wallet_id, amount, status, idempotency_key, created_at, updated_at
		 FROM transfers WHERE id = ?`, id,
	).Scan(&transfer.ID, &transfer.FromWalletID, &transfer.ToWalletID,
		&transfer.Amount, &transfer.Status, &transfer.IdempotencyKey,
		&transfer.CreatedAt, &transfer.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("transfer not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get transfer: %w", err)
	}
	return transfer, nil
}

// --- Ledger operations ---

func (r *SQLiteTxRepository) CreateLedgerEntry(ctx context.Context, entry *domain.LedgerEntry) error {
	_, err := r.tx.ExecContext(ctx,
		`INSERT INTO ledger_entries (id, wallet_id, transfer_id, entry_type, amount, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.WalletID, entry.TransferID,
		entry.EntryType, entry.Amount, entry.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create ledger entry: %w", err)
	}
	return nil
}

func (r *SQLiteTxRepository) GetLedgerEntriesByTransfer(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	rows, err := r.tx.QueryContext(ctx,
		`SELECT id, wallet_id, transfer_id, entry_type, amount, created_at
		 FROM ledger_entries WHERE transfer_id = ?`, transferID,
	)
	if err != nil {
		return nil, fmt.Errorf("get ledger entries: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.WalletID, &e.TransferID, &e.EntryType, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// --- Idempotency operations ---

func (r *SQLiteTxRepository) GetIdempotencyRecord(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	record := &domain.IdempotencyRecord{}
	err := r.tx.QueryRowContext(ctx,
		`SELECT idempotency_key, transfer_id, response_status, response_body, created_at
		 FROM idempotency_records WHERE idempotency_key = ?`, key,
	).Scan(&record.IdempotencyKey, &record.TransferID, &record.ResponseStatus,
		&record.ResponseBody, &record.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil // no record found is not an error
	}
	if err != nil {
		return nil, fmt.Errorf("get idempotency record: %w", err)
	}
	return record, nil
}

func (r *SQLiteTxRepository) CreateIdempotencyRecord(ctx context.Context, record *domain.IdempotencyRecord) error {
	_, err := r.tx.ExecContext(ctx,
		`INSERT INTO idempotency_records (idempotency_key, transfer_id, response_status, response_body, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		record.IdempotencyKey, record.TransferID, record.ResponseStatus,
		record.ResponseBody, record.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create idempotency record: %w", err)
	}
	return nil
}
