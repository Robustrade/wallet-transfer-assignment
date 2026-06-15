package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/ports"
)

type SQLiteStore struct {
	db *sql.DB
}

type SQLiteTx struct {
	conn *sql.Conn
}

const sqliteBusyTimeoutMillis = 5000

type scanner interface {
	Scan(dest ...any) error
}

type sqlitePragmaExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func OpenSQLite(ctx context.Context, dsn string) (*SQLiteStore, error) {
	dsn = normalizeSQLiteDSN(dsn)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err = applySQLiteConnectionPragmas(ctx, db); err != nil {
		closeErr := db.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("configure sqlite connection pragmas: %w; close db: %v", err, closeErr)
		}
		return nil, fmt.Errorf("configure sqlite connection pragmas: %w", err)
	}
	if _, err = db.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		closeErr := db.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("enable wal: %w; close db: %v", err, closeErr)
		}
		return nil, fmt.Errorf("enable wal: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS wallets (
			id TEXT PRIMARY KEY,
			balance INTEGER NOT NULL CHECK (balance >= 0),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS transfers (
			id TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL UNIQUE,
			request_hash TEXT NOT NULL,
			from_wallet_id TEXT NOT NULL,
			to_wallet_id TEXT NOT NULL,
			amount INTEGER NOT NULL CHECK (amount > 0),
			state TEXT NOT NULL CHECK (state IN ('PENDING', 'PROCESSED', 'FAILED')),
			error_code TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			CHECK (from_wallet_id <> to_wallet_id)
		);`,
		`CREATE TABLE IF NOT EXISTS ledger_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			wallet_id TEXT NOT NULL,
			transfer_id TEXT NOT NULL,
			entry_type TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
			amount INTEGER NOT NULL CHECK (amount > 0),
			created_at TEXT NOT NULL,
			FOREIGN KEY (wallet_id) REFERENCES wallets(id),
			FOREIGN KEY (transfer_id) REFERENCES transfers(id),
			UNIQUE (transfer_id, entry_type)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers(from_wallet_id);`,
		`CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet ON transfers(to_wallet_id);`,
		`CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet ON ledger_entries(wallet_id);`,
		`CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer ON ledger_entries(transfer_id);`,
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate sqlite: %w", err)
		}
	}

	return nil
}

func (s *SQLiteStore) WithTx(ctx context.Context, fn func(context.Context, ports.TransferRepository) error) error {
	return s.withTx(ctx, "BEGIN IMMEDIATE", fn)
}

func (s *SQLiteStore) WithReadTx(ctx context.Context, fn func(context.Context, ports.TransferRepository) error) error {
	return s.withTx(ctx, "BEGIN", fn)
}

func (s *SQLiteStore) withTx(ctx context.Context, beginStatement string, fn func(context.Context, ports.TransferRepository) error) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire sqlite connection: %w", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	if err = applySQLiteConnectionPragmas(ctx, conn); err != nil {
		return fmt.Errorf("configure sqlite connection pragmas: %w", err)
	}

	if _, err = conn.ExecContext(ctx, beginStatement); err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()

	tx := &SQLiteTx{conn: conn}
	if err = fn(ctx, tx); err != nil {
		return err
	}

	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true

	return nil
}

func (tx *SQLiteTx) CreateWallet(ctx context.Context, wallet domain.Wallet) error {
	_, err := tx.conn.ExecContext(
		ctx,
		`INSERT INTO wallets (id, balance, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		wallet.ID,
		wallet.Balance,
		formatTime(wallet.CreatedAt),
		formatTime(wallet.UpdatedAt),
	)
	if err != nil {
		if isUniquenessConstraintError(err) {
			return domain.NewAppError(domain.ErrorWalletAlreadyExists, "wallet already exists")
		}
		return fmt.Errorf("insert wallet: %w", err)
	}

	return nil
}

func (tx *SQLiteTx) GetWallet(ctx context.Context, id string) (domain.Wallet, bool, error) {
	row := tx.conn.QueryRowContext(
		ctx,
		`SELECT id, balance, created_at, updated_at FROM wallets WHERE id = ?`,
		id,
	)

	wallet, err := scanWallet(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Wallet{}, false, nil
	}
	if err != nil {
		return domain.Wallet{}, false, fmt.Errorf("get wallet: %w", err)
	}

	return wallet, true, nil
}

func (tx *SQLiteTx) FindTransferByID(ctx context.Context, id string) (domain.Transfer, bool, error) {
	row := tx.conn.QueryRowContext(ctx, selectTransferSQL()+` WHERE id = ?`, id)

	transfer, err := scanTransfer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Transfer{}, false, nil
	}
	if err != nil {
		return domain.Transfer{}, false, fmt.Errorf("find transfer by id: %w", err)
	}

	return transfer, true, nil
}

func (tx *SQLiteTx) FindTransferByIdempotencyKey(ctx context.Context, key string) (domain.Transfer, bool, error) {
	row := tx.conn.QueryRowContext(ctx, selectTransferSQL()+` WHERE idempotency_key = ?`, key)

	transfer, err := scanTransfer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Transfer{}, false, nil
	}
	if err != nil {
		return domain.Transfer{}, false, fmt.Errorf("find transfer by idempotency key: %w", err)
	}

	return transfer, true, nil
}

func (tx *SQLiteTx) InsertTransfer(ctx context.Context, transfer domain.Transfer) error {
	_, err := tx.conn.ExecContext(
		ctx,
		`INSERT INTO transfers (
			id, idempotency_key, request_hash, from_wallet_id, to_wallet_id,
			amount, state, error_code, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)`,
		transfer.ID,
		transfer.IdempotencyKey,
		transfer.RequestHash,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		string(transfer.State),
		formatTime(transfer.CreatedAt),
		formatTime(transfer.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert transfer: %w", err)
	}

	return nil
}

func (tx *SQLiteTx) UpdateTransferState(
	ctx context.Context,
	id string,
	state domain.TransferState,
	errorCode domain.ErrorCode,
	updatedAt time.Time,
) (domain.Transfer, error) {
	var nullableErrorCode any
	if errorCode != "" {
		nullableErrorCode = string(errorCode)
	}

	result, err := tx.conn.ExecContext(
		ctx,
		`UPDATE transfers
		 SET state = ?, error_code = ?, updated_at = ?
		 WHERE id = ? AND state = ?`,
		string(state),
		nullableErrorCode,
		formatTime(updatedAt),
		id,
		string(domain.TransferPending),
	)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("update transfer state: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("update transfer state rows affected: %w", err)
	}
	if rowsAffected != 1 {
		return domain.Transfer{}, domain.NewAppError(domain.ErrorInvalidTransferState, "transfer is not pending")
	}

	transfer, found, err := tx.FindTransferByID(ctx, id)
	if err != nil {
		return domain.Transfer{}, err
	}
	if !found {
		return domain.Transfer{}, domain.NewAppError(domain.ErrorTransferNotFound, "transfer not found")
	}

	return transfer, nil
}

func (tx *SQLiteTx) DebitWallet(ctx context.Context, walletID string, amount int64, updatedAt time.Time) (bool, error) {
	result, err := tx.conn.ExecContext(
		ctx,
		`UPDATE wallets
		 SET balance = balance - ?, updated_at = ?
		 WHERE id = ? AND balance >= ?`,
		amount,
		formatTime(updatedAt),
		walletID,
		amount,
	)
	if err != nil {
		return false, fmt.Errorf("debit wallet: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("debit wallet rows affected: %w", err)
	}

	return rowsAffected == 1, nil
}

func (tx *SQLiteTx) CreditWallet(ctx context.Context, walletID string, amount int64, updatedAt time.Time) error {
	result, err := tx.conn.ExecContext(
		ctx,
		`UPDATE wallets SET balance = balance + ?, updated_at = ? WHERE id = ?`,
		amount,
		formatTime(updatedAt),
		walletID,
	)
	if err != nil {
		return fmt.Errorf("credit wallet: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("credit wallet rows affected: %w", err)
	}
	if rowsAffected != 1 {
		return domain.NewAppError(domain.ErrorDestinationWalletNotFound, "destination wallet not found")
	}

	return nil
}

func (tx *SQLiteTx) InsertLedgerEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	for _, entry := range entries {
		_, err := tx.conn.ExecContext(
			ctx,
			`INSERT INTO ledger_entries (wallet_id, transfer_id, entry_type, amount, created_at)
			 VALUES (?, ?, ?, ?, ?)`,
			entry.WalletID,
			entry.TransferID,
			string(entry.Type),
			entry.Amount,
			formatTime(entry.CreatedAt),
		)
		if err != nil {
			return fmt.Errorf("insert ledger entry: %w", err)
		}
	}

	return nil
}

func (tx *SQLiteTx) ListLedgerEntriesByTransfer(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	rows, err := tx.conn.QueryContext(
		ctx,
		`SELECT id, wallet_id, transfer_id, entry_type, amount, created_at
		 FROM ledger_entries
		 WHERE transfer_id = ?
		 ORDER BY id ASC`,
		transferID,
	)
	if err != nil {
		return nil, fmt.Errorf("list ledger entries: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	entries := make([]domain.LedgerEntry, 0, 2)
	for rows.Next() {
		var (
			entry     domain.LedgerEntry
			entryType string
			createdAt string
		)
		if err = rows.Scan(&entry.ID, &entry.WalletID, &entry.TransferID, &entryType, &entry.Amount, &createdAt); err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", err)
		}

		entry.Type = domain.LedgerEntryType(entryType)
		entry.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse ledger entry created_at: %w", err)
		}
		entries = append(entries, entry)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ledger entries: %w", err)
	}

	return entries, nil
}

func scanWallet(row scanner) (domain.Wallet, error) {
	var (
		wallet    domain.Wallet
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&wallet.ID, &wallet.Balance, &createdAt, &updatedAt); err != nil {
		return domain.Wallet{}, err
	}

	var err error
	wallet.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("parse wallet created_at: %w", err)
	}

	wallet.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("parse wallet updated_at: %w", err)
	}

	return wallet, nil
}

func scanTransfer(row scanner) (domain.Transfer, error) {
	var (
		transfer  domain.Transfer
		state     string
		errorCode sql.NullString
		createdAt string
		updatedAt string
	)
	if err := row.Scan(
		&transfer.ID,
		&transfer.IdempotencyKey,
		&transfer.RequestHash,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&state,
		&errorCode,
		&createdAt,
		&updatedAt,
	); err != nil {
		return domain.Transfer{}, err
	}

	transfer.State = domain.TransferState(state)
	if errorCode.Valid {
		transfer.ErrorCode = domain.ErrorCode(errorCode.String)
	}

	var err error
	transfer.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("parse transfer created_at: %w", err)
	}

	transfer.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("parse transfer updated_at: %w", err)
	}

	return transfer, nil
}

func selectTransferSQL() string {
	return `SELECT id, idempotency_key, request_hash, from_wallet_id, to_wallet_id,
		amount, state, error_code, created_at, updated_at FROM transfers`
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

func applySQLiteConnectionPragmas(ctx context.Context, execer sqlitePragmaExecutor) error {
	if _, err := execer.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := execer.ExecContext(
		ctx,
		fmt.Sprintf("PRAGMA busy_timeout = %d", sqliteBusyTimeoutMillis),
	); err != nil {
		return fmt.Errorf("set busy timeout: %w", err)
	}

	return nil
}

func isUniquenessConstraintError(err error) bool {
	var sqliteErr sqlite3.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}

	return sqliteErr.ExtendedCode == sqlite3.ErrConstraintPrimaryKey ||
		sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique
}

func normalizeSQLiteDSN(dsn string) string {
	if !strings.HasPrefix(dsn, "file:") {
		return dsn
	}

	dsn = appendSQLiteParamIfMissing(dsn, "_busy_timeout=5000", "_busy_timeout")
	dsn = appendSQLiteParamIfMissing(dsn, "_foreign_keys=on", "_foreign_keys", "_fk")

	return dsn
}

func appendSQLiteParamIfMissing(dsn string, param string, keys ...string) string {
	for _, key := range keys {
		if strings.Contains(dsn, key+"=") {
			return dsn
		}
	}

	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}

	return dsn + separator + param
}
