package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/wallet-transfer-assignment/internal/domain"
)

// PostgresRepository implements Repository for PostgreSQL.
type PostgresRepository struct {
	db *sql.DB
}

// PostgresTxRepository is a transaction-scoped repository for PostgreSQL.
type PostgresTxRepository struct {
	tx *sql.Tx
}

// NewPostgresRepository creates a new PostgreSQL repository.
func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// --- TxManager ---

// WithTransaction executes fn within a PostgreSQL SERIALIZABLE transaction.
func (r *PostgresRepository) WithTransaction(ctx context.Context, fn func(repo Repository) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelSerializable,
	})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	txRepo := &PostgresTxRepository{tx: tx}

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

func (r *PostgresTxRepository) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	wallet := &domain.Wallet{}
	err := r.tx.QueryRowContext(ctx,
		"SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1", id,
	).Scan(&wallet.ID, &wallet.Balance, &wallet.CreatedAt, &wallet.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, domain.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}
	return wallet, nil
}

// GetWalletForUpdate locks the wallet row using SELECT ... FOR UPDATE.
// Wallets should be locked in a consistent order (by ID) to prevent deadlocks.
func (r *PostgresTxRepository) GetWalletForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	wallet := &domain.Wallet{}
	err := r.tx.QueryRowContext(ctx,
		"SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1 FOR UPDATE", id,
	).Scan(&wallet.ID, &wallet.Balance, &wallet.CreatedAt, &wallet.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, domain.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get wallet for update: %w", err)
	}
	return wallet, nil
}

func (r *PostgresTxRepository) DebitWallet(ctx context.Context, id string, amount int64) error {
	result, err := r.tx.ExecContext(ctx,
		"UPDATE wallets SET balance = balance - $1, updated_at = $2 WHERE id = $3 AND balance >= $1",
		amount, time.Now().UTC(), id,
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

func (r *PostgresTxRepository) CreditWallet(ctx context.Context, id string, amount int64) error {
	_, err := r.tx.ExecContext(ctx,
		"UPDATE wallets SET balance = balance + $1, updated_at = $2 WHERE id = $3",
		amount, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("credit wallet: %w", err)
	}
	return nil
}

// --- Transfer operations ---

func (r *PostgresTxRepository) CreateTransfer(ctx context.Context, transfer *domain.Transfer) error {
	_, err := r.tx.ExecContext(ctx,
		`INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, idempotency_key, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		transfer.ID, transfer.FromWalletID, transfer.ToWalletID,
		transfer.Amount, transfer.Status, transfer.IdempotencyKey,
		transfer.CreatedAt, transfer.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create transfer: %w", err)
	}
	return nil
}

func (r *PostgresTxRepository) UpdateTransferStatus(ctx context.Context, id string, status domain.TransferStatus) error {
	_, err := r.tx.ExecContext(ctx,
		"UPDATE transfers SET status = $1, updated_at = $2 WHERE id = $3",
		status, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}
	return nil
}

func (r *PostgresTxRepository) GetTransfer(ctx context.Context, id string) (*domain.Transfer, error) {
	transfer := &domain.Transfer{}
	err := r.tx.QueryRowContext(ctx,
		`SELECT id, from_wallet_id, to_wallet_id, amount, status, idempotency_key, created_at, updated_at
		 FROM transfers WHERE id = $1`, id,
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

func (r *PostgresTxRepository) CreateLedgerEntry(ctx context.Context, entry *domain.LedgerEntry) error {
	_, err := r.tx.ExecContext(ctx,
		`INSERT INTO ledger_entries (id, wallet_id, transfer_id, entry_type, amount, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		entry.ID, entry.WalletID, entry.TransferID,
		entry.EntryType, entry.Amount, entry.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create ledger entry: %w", err)
	}
	return nil
}

func (r *PostgresTxRepository) GetLedgerEntriesByTransfer(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	rows, err := r.tx.QueryContext(ctx,
		`SELECT id, wallet_id, transfer_id, entry_type, amount, created_at
		 FROM ledger_entries WHERE transfer_id = $1`, transferID,
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

func (r *PostgresTxRepository) GetIdempotencyRecord(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	record := &domain.IdempotencyRecord{}
	err := r.tx.QueryRowContext(ctx,
		`SELECT idempotency_key, transfer_id, response_status, response_body, created_at
		 FROM idempotency_records WHERE idempotency_key = $1`, key,
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

func (r *PostgresTxRepository) CreateIdempotencyRecord(ctx context.Context, record *domain.IdempotencyRecord) error {
	_, err := r.tx.ExecContext(ctx,
		`INSERT INTO idempotency_records (idempotency_key, transfer_id, response_status, response_body, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		record.IdempotencyKey, record.TransferID, record.ResponseStatus,
		record.ResponseBody, record.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create idempotency record: %w", err)
	}
	return nil
}

// LockWalletsInOrder locks two wallets in consistent ID order to prevent deadlocks.
// This is a helper used by the service layer for PostgreSQL.
func LockWalletsInOrder(ctx context.Context, repo Repository, id1, id2 string) (*domain.Wallet, *domain.Wallet, error) {
	ids := []string{id1, id2}
	sort.Strings(ids)

	w1, err := repo.GetWalletForUpdate(ctx, ids[0])
	if err != nil {
		return nil, nil, err
	}
	w2, err := repo.GetWalletForUpdate(ctx, ids[1])
	if err != nil {
		return nil, nil, err
	}

	// Return in the original order requested
	if ids[0] == id1 {
		return w1, w2, nil
	}
	return w2, w1, nil
}
