package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dale-tomson/wallet-transfer-assignment/internal/domain"
	"github.com/dale-tomson/wallet-transfer-assignment/migrations"
	_ "modernc.org/sqlite"
)

var initMigrationSQL = migrations.InitSQL

// NewSQLiteDB initializes a SQLite database connection pool optimized for concurrent execution
func NewSQLiteDB(dsn string) (*sql.DB, error) {
	if dsn == "" {
		dsn = "wallet.db"
	}

	if !strings.Contains(dsn, "_txlock") {
		if strings.Contains(dsn, "?") {
			dsn += "&_txlock=immediate"
		} else {
			dsn += "?_txlock=immediate"
		}
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite connection: %w", err)
	}

	// Serializes writes through one connection to eliminate database file lock contention
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Apply critical performance & integrity PRAGMAs
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA synchronous = NORMAL;",
	}

	for _, pragma := range pragmas {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to execute %q: %w", pragma, err)
		}
	}

	// Run initial schema migration
	if _, err := db.ExecContext(ctx, initMigrationSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run initial migration: %w", err)
	}

	return db, nil
}

func getExecer(db *sql.DB, tx *sql.Tx) queryExecer {
	if tx != nil {
		return tx
	}
	return db
}

func isConstraintCheckError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "check constraint") || strings.Contains(msg, "chk_wallet_balance")
}

// =============================================================================
// Transaction Manager
// =============================================================================

// SQLTxManager manages atomic database transactions
type SQLTxManager struct {
	db *sql.DB
}

// NewSQLTxManager creates a new SQLTxManager
func NewSQLTxManager(db *sql.DB) *SQLTxManager {
	return &SQLTxManager{db: db}
}

// RunInTx executes a function within a database transaction with BEGIN IMMEDIATE guarantees
func (m *SQLTxManager) RunInTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := m.db.BeginTx(ctx, &sql.TxOptions{})
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
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("tx error: %w, rollback error: %v", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// =============================================================================
// Wallet Repository
// =============================================================================

type SQLWalletRepository struct {
	db *sql.DB
}

func NewSQLWalletRepository(db *sql.DB) *SQLWalletRepository {
	return &SQLWalletRepository{db: db}
}

func (r *SQLWalletRepository) Create(ctx context.Context, tx *sql.Tx, wallet *domain.Wallet) error {
	return r.CreateWallet(ctx, tx, wallet)
}

func (r *SQLWalletRepository) GetByID(ctx context.Context, tx *sql.Tx, id string) (*domain.Wallet, error) {
	return r.GetWalletForUpdate(ctx, tx, id)
}

func (r *SQLWalletRepository) CreateWallet(ctx context.Context, tx *sql.Tx, wallet *domain.Wallet) error {
	execer := getExecer(r.db, tx)
	query := `
		INSERT INTO wallets (id, user_id, currency, balance, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?);
	`
	_, err := execer.ExecContext(ctx, query,
		wallet.ID,
		wallet.UserID,
		wallet.Currency,
		wallet.Balance,
		wallet.CreatedAt,
		wallet.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert wallet: %w", err)
	}
	return nil
}

func (r *SQLWalletRepository) GetWalletForUpdate(ctx context.Context, tx *sql.Tx, id string) (*domain.Wallet, error) {
	execer := getExecer(r.db, tx)
	query := `
		SELECT id, user_id, currency, balance, created_at, updated_at
		FROM wallets
		WHERE id = ?;
	`
	var w domain.Wallet
	err := execer.QueryRowContext(ctx, query, id).Scan(
		&w.ID,
		&w.UserID,
		&w.Currency,
		&w.Balance,
		&w.CreatedAt,
		&w.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("failed to query wallet: %w", err)
	}
	return &w, nil
}

func (r *SQLWalletRepository) UpdateBalances(ctx context.Context, tx *sql.Tx, fromWalletID, toWalletID string, amount int64) error {
	if fromWalletID == toWalletID {
		return domain.ErrSelfTransfer
	}
	if amount <= 0 {
		return domain.ErrInvalidAmount
	}

	execer := getExecer(r.db, tx)
	now := time.Now()

	debitStmt := `UPDATE wallets SET balance = balance - ?, updated_at = ? WHERE id = ?;`
	creditStmt := `UPDATE wallets SET balance = balance + ?, updated_at = ? WHERE id = ?;`

	// Deterministic lock/update order (lexicographically sorted wallet IDs)
	if fromWalletID < toWalletID {
		res, err := execer.ExecContext(ctx, debitStmt, amount, now, fromWalletID)
		if err != nil {
			if isConstraintCheckError(err) {
				return fmt.Errorf("%w: %v", domain.ErrInsufficientFunds, err)
			}
			return fmt.Errorf("failed to debit wallet: %w", err)
		}
		rows, err := res.RowsAffected()
		if err != nil || rows == 0 {
			return domain.ErrWalletNotFound
		}

		res, err = execer.ExecContext(ctx, creditStmt, amount, now, toWalletID)
		if err != nil {
			return fmt.Errorf("failed to credit wallet: %w", err)
		}
		rows, err = res.RowsAffected()
		if err != nil || rows == 0 {
			return domain.ErrWalletNotFound
		}
	} else {
		res, err := execer.ExecContext(ctx, creditStmt, amount, now, toWalletID)
		if err != nil {
			return fmt.Errorf("failed to credit wallet: %w", err)
		}
		rows, err := res.RowsAffected()
		if err != nil || rows == 0 {
			return domain.ErrWalletNotFound
		}

		res, err = execer.ExecContext(ctx, debitStmt, amount, now, fromWalletID)
		if err != nil {
			if isConstraintCheckError(err) {
				return fmt.Errorf("%w: %v", domain.ErrInsufficientFunds, err)
			}
			return fmt.Errorf("failed to debit wallet: %w", err)
		}
		rows, err = res.RowsAffected()
		if err != nil || rows == 0 {
			return domain.ErrWalletNotFound
		}
	}

	return nil
}

// =============================================================================
// Transfer Repository
// =============================================================================

type SQLTransferRepository struct {
	db *sql.DB
}

func NewSQLTransferRepository(db *sql.DB) *SQLTransferRepository {
	return &SQLTransferRepository{db: db}
}

func (r *SQLTransferRepository) CreateTransferAndEntries(
	ctx context.Context,
	tx *sql.Tx,
	transfer *domain.Transfer,
	debit *domain.LedgerEntry,
	credit *domain.LedgerEntry,
) error {
	execer := getExecer(r.db, tx)

	if transfer.Status == "" {
		transfer.Status = domain.TransferStatusCompleted
	}

	transferQuery := `
		INSERT INTO transfers (
			id, idempotency_key, from_wallet_id, to_wallet_id, amount, currency, status, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?);
	`
	_, err := execer.ExecContext(ctx, transferQuery,
		transfer.ID,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		transfer.Currency,
		string(transfer.Status),
		transfer.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert transfer record: %w", err)
	}

	ledgerQuery := `
		INSERT INTO ledger_entries (
			id, transfer_id, wallet_id, amount, direction, created_at
		) VALUES (?, ?, ?, ?, ?, ?);
	`

	_, err = execer.ExecContext(ctx, ledgerQuery,
		debit.ID,
		transfer.ID,
		debit.WalletID,
		debit.Amount,
		string(domain.DirectionDebit),
		debit.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert debit ledger entry: %w", err)
	}

	_, err = execer.ExecContext(ctx, ledgerQuery,
		credit.ID,
		transfer.ID,
		credit.WalletID,
		credit.Amount,
		string(domain.DirectionCredit),
		credit.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert credit ledger entry: %w", err)
	}

	return nil
}

// =============================================================================
// Idempotency Repository
// =============================================================================

type SQLIdempotencyRepository struct {
	db *sql.DB
}

func NewSQLIdempotencyRepository(db *sql.DB) *SQLIdempotencyRepository {
	return &SQLIdempotencyRepository{db: db}
}

func (r *SQLIdempotencyRepository) Reserve(
	ctx context.Context,
	key string,
	requestHash string,
	ttl time.Duration,
) (*domain.IdempotencyRecord, error) {
	if key == "" {
		return nil, domain.ErrIdempotencyKeyRequired
	}

	now := time.Now().UTC()
	lockUntil := now.Add(ttl)

	selectQuery := `
		SELECT key, request_hash, status, response_code, response_body, created_at, locked_until
		FROM idempotency_records
		WHERE key = ?;
	`

	var (
		rec      domain.IdempotencyRecord
		status   string
		respCode sql.NullInt64
		respBody sql.NullString
	)

	err := r.db.QueryRowContext(ctx, selectQuery, key).Scan(
		&rec.Key,
		&rec.RequestHash,
		&status,
		&respCode,
		&respBody,
		&rec.CreatedAt,
		&rec.LockedUntil,
	)

	if err == nil {
		rec.Status = domain.IdempotencyStatus(status)
		if respCode.Valid {
			rec.ResponseCode = int(respCode.Int64)
		}
		if respBody.Valid {
			rec.ResponseBody = respBody.String
		}

		switch rec.Status {
		case domain.IdempotencyStatusResolved:
			if rec.RequestHash != requestHash {
				return nil, domain.ErrIdempotencyKeyConflict
			}
			return &rec, nil

		case domain.IdempotencyStatusInProgress:
			if rec.LockedUntil.After(now) {
				return nil, domain.ErrConcurrentRequest
			}
			// Lease expired (crashed process) - reclaim lease
			updateQuery := `
				UPDATE idempotency_records
				SET request_hash = ?, status = 'IN_PROGRESS', locked_until = ?, response_code = NULL, response_body = NULL
				WHERE key = ?;
			`
			if _, err := r.db.ExecContext(ctx, updateQuery, requestHash, lockUntil, key); err != nil {
				return nil, fmt.Errorf("failed to reclaim expired idempotency lease: %w", err)
			}
			rec.RequestHash = requestHash
			rec.Status = domain.IdempotencyStatusInProgress
			rec.LockedUntil = lockUntil
			rec.ResponseCode = 0
			rec.ResponseBody = ""
			return &rec, nil

		case domain.IdempotencyStatusFailed:
			// Failed transaction retry - re-reserve key
			updateQuery := `
				UPDATE idempotency_records
				SET request_hash = ?, status = 'IN_PROGRESS', locked_until = ?, response_code = NULL, response_body = NULL
				WHERE key = ?;
			`
			if _, err := r.db.ExecContext(ctx, updateQuery, requestHash, lockUntil, key); err != nil {
				return nil, fmt.Errorf("failed to re-reserve failed idempotency key: %w", err)
			}
			rec.RequestHash = requestHash
			rec.Status = domain.IdempotencyStatusInProgress
			rec.LockedUntil = lockUntil
			rec.ResponseCode = 0
			rec.ResponseBody = ""
			return &rec, nil

		default:
			return nil, fmt.Errorf("unexpected idempotency status %q", rec.Status)
		}
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to query idempotency record: %w", err)
	}

	// Insert new in-progress record
	insertQuery := `
		INSERT INTO idempotency_records (key, request_hash, status, created_at, locked_until)
		VALUES (?, ?, 'IN_PROGRESS', ?, ?);
	`
	if _, err := r.db.ExecContext(ctx, insertQuery, key, requestHash, now, lockUntil); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			// Another concurrent request just inserted this key. Re-query to return cached response or ErrConcurrentRequest.
			return r.Reserve(ctx, key, requestHash, ttl)
		}
		return nil, fmt.Errorf("failed to insert idempotency record: %w", err)
	}

	return &domain.IdempotencyRecord{
		Key:         key,
		RequestHash: requestHash,
		Status:      domain.IdempotencyStatusInProgress,
		CreatedAt:   now,
		LockedUntil: lockUntil,
	}, nil
}

func (r *SQLIdempotencyRepository) Resolve(ctx context.Context, tx *sql.Tx, key string, code int, responseBody string) error {
	execer := getExecer(r.db, tx)
	query := `
		UPDATE idempotency_records
		SET status = 'RESOLVED', response_code = ?, response_body = ?
		WHERE key = ?;
	`
	res, err := execer.ExecContext(ctx, query, code, responseBody, key)
	if err != nil {
		return fmt.Errorf("failed to resolve idempotency key: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil || rows == 0 {
		return fmt.Errorf("idempotency record %q not found to resolve", key)
	}
	return nil
}

func (r *SQLIdempotencyRepository) Release(ctx context.Context, key string) error {
	return r.ReleaseOrSetFailed(ctx, nil, key)
}

func (r *SQLIdempotencyRepository) ReleaseOrSetFailed(ctx context.Context, tx *sql.Tx, key string) error {
	execer := getExecer(r.db, tx)
	query := `
		UPDATE idempotency_records
		SET status = 'FAILED'
		WHERE key = ?;
	`
	_, err := execer.ExecContext(ctx, query, key)
	if err != nil {
		return fmt.Errorf("failed to mark idempotency key as failed: %w", err)
	}
	return nil
}
