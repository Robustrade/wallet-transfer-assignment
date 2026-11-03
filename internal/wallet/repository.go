package wallet

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS wallets (
    wallet_id TEXT PRIMARY KEY,
    balance_minor INTEGER NOT NULL CHECK (balance_minor >= 0),
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS transfers (
    transfer_id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    request_hash TEXT NOT NULL,
    from_wallet_id TEXT NOT NULL REFERENCES wallets(wallet_id),
    to_wallet_id TEXT NOT NULL REFERENCES wallets(wallet_id),
    amount_minor INTEGER NOT NULL CHECK (amount_minor > 0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_code TEXT,
    created_at TEXT NOT NULL,
    CHECK (from_wallet_id <> to_wallet_id),
    CHECK (
        (status = 'FAILED' AND failure_code IS NOT NULL)
        OR (status <> 'FAILED' AND failure_code IS NULL)
    )
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    entry_id INTEGER PRIMARY KEY AUTOINCREMENT,
    wallet_id TEXT NOT NULL REFERENCES wallets(wallet_id),
    transfer_id TEXT REFERENCES transfers(transfer_id),
    entry_type TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount_minor INTEGER NOT NULL CHECK (amount_minor > 0),
    created_at TEXT NOT NULL,
    CHECK (transfer_id IS NOT NULL OR entry_type = 'CREDIT'),
    UNIQUE (transfer_id, entry_type)
);

CREATE INDEX IF NOT EXISTS idx_ledger_wallet_created
    ON ledger_entries(wallet_id, created_at);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ledger_wallet_opening
    ON ledger_entries(wallet_id)
    WHERE transfer_id IS NULL;

CREATE TRIGGER IF NOT EXISTS validate_transfer_ledger_entry
BEFORE INSERT ON ledger_entries
WHEN NEW.transfer_id IS NOT NULL
BEGIN
    SELECT CASE
        WHEN (SELECT status FROM transfers WHERE transfer_id = NEW.transfer_id) <> 'PENDING'
        THEN RAISE(ABORT, 'ledger entries require a pending transfer')
        WHEN NEW.amount_minor <> (
            SELECT amount_minor FROM transfers WHERE transfer_id = NEW.transfer_id
        )
        THEN RAISE(ABORT, 'ledger amount does not match transfer')
        WHEN NEW.entry_type = 'DEBIT' AND NEW.wallet_id <> (
            SELECT from_wallet_id FROM transfers WHERE transfer_id = NEW.transfer_id
        )
        THEN RAISE(ABORT, 'debit wallet does not match transfer source')
        WHEN NEW.entry_type = 'CREDIT' AND NEW.wallet_id <> (
            SELECT to_wallet_id FROM transfers WHERE transfer_id = NEW.transfer_id
        )
        THEN RAISE(ABORT, 'credit wallet does not match transfer destination')
    END;
END;

CREATE TRIGGER IF NOT EXISTS validate_processed_transfer
BEFORE UPDATE OF status ON transfers
WHEN OLD.status = 'PENDING' AND NEW.status = 'PROCESSED'
BEGIN
    SELECT CASE
        WHEN (
            SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = NEW.transfer_id
        ) <> 2
        THEN RAISE(ABORT, 'processed transfers require exactly two ledger entries')
        WHEN (
            SELECT COUNT(*) FROM ledger_entries
            WHERE transfer_id = NEW.transfer_id
              AND entry_type = 'DEBIT'
              AND wallet_id = NEW.from_wallet_id
              AND amount_minor = NEW.amount_minor
        ) <> 1
        THEN RAISE(ABORT, 'processed transfer is missing its matching debit')
        WHEN (
            SELECT COUNT(*) FROM ledger_entries
            WHERE transfer_id = NEW.transfer_id
              AND entry_type = 'CREDIT'
              AND wallet_id = NEW.to_wallet_id
              AND amount_minor = NEW.amount_minor
        ) <> 1
        THEN RAISE(ABORT, 'processed transfer is missing its matching credit')
    END;
END;

CREATE TRIGGER IF NOT EXISTS validate_failed_transfer
BEFORE UPDATE OF status ON transfers
WHEN OLD.status = 'PENDING' AND NEW.status = 'FAILED'
BEGIN
    SELECT CASE
        WHEN (
            SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = NEW.transfer_id
        ) <> 0
        THEN RAISE(ABORT, 'failed transfers cannot have ledger entries')
    END;
END;

CREATE TRIGGER IF NOT EXISTS validate_transfer_state_transition
BEFORE UPDATE OF status ON transfers
WHEN NOT (OLD.status = 'PENDING' AND NEW.status IN ('PROCESSED', 'FAILED'))
BEGIN
    SELECT RAISE(ABORT, 'invalid transfer state transition');
END;

CREATE TRIGGER IF NOT EXISTS prevent_ledger_entry_update
BEFORE UPDATE ON ledger_entries
BEGIN
    SELECT RAISE(ABORT, 'ledger entries are immutable');
END;

CREATE TRIGGER IF NOT EXISTS prevent_ledger_entry_delete
BEFORE DELETE ON ledger_entries
BEGIN
    SELECT RAISE(ABORT, 'ledger entries are immutable');
END;
`

type Repository struct {
	db *sql.DB
}

func OpenRepository(databasePath string) (*Repository, error) {
	if databasePath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(databasePath), 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 30000",
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			return nil, fmt.Errorf("configure SQLite: %w", err)
		}
	}
	if databasePath != ":memory:" {
		if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
			db.Close()
			return nil, fmt.Errorf("enable SQLite WAL mode: %w", err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize SQLite schema: %w", err)
	}
	return &Repository{db: db}, nil
}

func (repository *Repository) Close() error {
	return repository.db.Close()
}

func (repository *Repository) withImmediateTransaction(
	ctx context.Context,
	operation func(*sql.Conn) error,
) (err error) {
	connection, err := repository.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("get database connection: %w", err)
	}
	defer connection.Close()

	if _, err = connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin transfer transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, rollbackErr := connection.ExecContext(context.Background(), "ROLLBACK")
			if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				err = errors.Join(err, fmt.Errorf("roll back transaction: %w", rollbackErr))
			}
		}
	}()

	if err = operation(connection); err != nil {
		return err
	}
	if _, err = connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}

func (repository *Repository) createWallet(
	ctx context.Context,
	connection *sql.Conn,
	wallet Wallet,
) error {
	var existing string
	err := connection.QueryRowContext(
		ctx,
		"SELECT wallet_id FROM wallets WHERE wallet_id = ?",
		wallet.WalletID,
	).Scan(&existing)
	if err == nil {
		return fmt.Errorf("%w: walletId already exists", ErrConflict)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check wallet identifier: %w", err)
	}

	_, err = connection.ExecContext(
		ctx,
		"INSERT INTO wallets (wallet_id, balance_minor, created_at) VALUES (?, ?, ?)",
		wallet.WalletID,
		int64(wallet.Balance),
		wallet.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert wallet: %w", err)
	}
	if wallet.Balance > 0 {
		_, err = connection.ExecContext(
			ctx,
			`INSERT INTO ledger_entries
				(wallet_id, transfer_id, entry_type, amount_minor, created_at)
			 VALUES (?, NULL, 'CREDIT', ?, ?)`,
			wallet.WalletID,
			int64(wallet.Balance),
			wallet.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("record opening balance: %w", err)
		}
	}
	return nil
}

func (repository *Repository) getWallet(
	ctx context.Context,
	queryer interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	walletID string,
) (Wallet, error) {
	var wallet Wallet
	err := queryer.QueryRowContext(
		ctx,
		"SELECT wallet_id, balance_minor, created_at FROM wallets WHERE wallet_id = ?",
		walletID,
	).Scan(&wallet.WalletID, (*int64)(&wallet.Balance), &wallet.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Wallet{}, fmt.Errorf("%w: wallet not found", ErrNotFound)
	}
	if err != nil {
		return Wallet{}, fmt.Errorf("read wallet: %w", err)
	}
	return wallet, nil
}

func (repository *Repository) getTransferByKey(
	ctx context.Context,
	connection *sql.Conn,
	key string,
) (TransferResult, error) {
	return scanTransfer(connection.QueryRowContext(ctx, transferSelect+` WHERE idempotency_key = ?`, key))
}

func (repository *Repository) getTransfer(
	ctx context.Context,
	connection *sql.Conn,
	transferID string,
) (TransferResult, error) {
	return scanTransfer(connection.QueryRowContext(ctx, transferSelect+` WHERE transfer_id = ?`, transferID))
}

const transferSelect = `SELECT transfer_id, idempotency_key, request_hash,
	from_wallet_id, to_wallet_id, amount_minor, status, failure_code, created_at
	FROM transfers`

func scanTransfer(row *sql.Row) (TransferResult, error) {
	var transfer TransferResult
	var amount int64
	var failureCode sql.NullString
	err := row.Scan(
		&transfer.TransferID,
		&transfer.IdempotencyKey,
		&transfer.requestHash,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&amount,
		&transfer.Status,
		&failureCode,
		&transfer.CreatedAt,
	)
	if err != nil {
		return TransferResult{}, err
	}
	transfer.Amount = Money(amount)
	if failureCode.Valid {
		transfer.FailureCode = &failureCode.String
	}
	return transfer, nil
}

func (repository *Repository) insertTransfer(
	ctx context.Context,
	connection *sql.Conn,
	transfer TransferResult,
) error {
	_, err := connection.ExecContext(
		ctx,
		`INSERT INTO transfers (
			transfer_id, idempotency_key, request_hash, from_wallet_id, to_wallet_id,
			amount_minor, status, failure_code, created_at
		) VALUES (?, ?, ?, ?, ?, ?, 'PENDING', NULL, ?)`,
		transfer.TransferID,
		transfer.IdempotencyKey,
		transfer.requestHash,
		transfer.FromWalletID,
		transfer.ToWalletID,
		int64(transfer.Amount),
		transfer.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert pending transfer: %w", err)
	}
	return nil
}

func (repository *Repository) updateBalance(
	ctx context.Context,
	connection *sql.Conn,
	walletID string,
	balance Money,
) error {
	_, err := connection.ExecContext(
		ctx,
		"UPDATE wallets SET balance_minor = ? WHERE wallet_id = ?",
		int64(balance),
		walletID,
	)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}
	return nil
}

func (repository *Repository) insertLedgerEntry(
	ctx context.Context,
	connection *sql.Conn,
	walletID, transferID, entryType string,
	amount Money,
	createdAt string,
) error {
	_, err := connection.ExecContext(
		ctx,
		`INSERT INTO ledger_entries
			(wallet_id, transfer_id, entry_type, amount_minor, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		walletID,
		transferID,
		entryType,
		int64(amount),
		createdAt,
	)
	if err != nil {
		return fmt.Errorf("insert %s ledger entry: %w", entryType, err)
	}
	return nil
}

func (repository *Repository) finishTransfer(
	ctx context.Context,
	connection *sql.Conn,
	transferID, status string,
	failureCode *string,
) error {
	_, err := connection.ExecContext(
		ctx,
		"UPDATE transfers SET status = ?, failure_code = ? WHERE transfer_id = ? AND status = 'PENDING'",
		status,
		failureCode,
		transferID,
	)
	if err != nil {
		return fmt.Errorf("update transfer state: %w", err)
	}
	return nil
}
