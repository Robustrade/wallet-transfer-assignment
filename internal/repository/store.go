package repository

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/domain"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaFS embed.FS

const timeLayout = time.RFC3339Nano

// Store is the SQLite persistence boundary. All SQL lives here.
type Store struct {
	db *sql.DB
}

func MemoryDSN(name string) string {
	return "file:" + name + "?mode=memory&cache=shared&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate"
}

func FileDSN(path string) string {
	return "file:" + path + "?_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_txlock=immediate"
}

func Open(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	return &Store{db: db}, nil
}

// OpenConcurrent is used by concurrency tests. WAL plus a small pool lets
// overlapping transactions contend instead of queueing on one connection.
func OpenConcurrent(dsn string) (*Store, error) {
	s, err := Open(dsn)
	if err != nil {
		return nil, err
	}
	s.db.SetMaxOpenConns(8)
	if _, err := s.db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("enable wal: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Migrate() error {
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}
	if _, err := s.db.Exec(string(schema)); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

// Tx is the unit of work handed to the service. It exposes persistence
// operations only.
type Tx struct {
	tx *sql.Tx
}

func (s *Store) WithTx(ctx context.Context, fn func(*Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(&Tx{tx: tx}); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *Store) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	return scanWallet(s.db.QueryRowContext(ctx, walletSelect+" WHERE id = ?", id))
}

func (s *Store) ListLedgerByTransfer(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	return queryLedger(ctx, s.db, `SELECT id, transfer_id, wallet_id, entry_type, amount, created_at
		FROM ledger_entries WHERE transfer_id = ? ORDER BY entry_type`, transferID)
}

func (s *Store) CountTransfers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM transfers`).Scan(&n)
	return n, err
}

func (s *Store) CountLedgerEntries(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_entries`).Scan(&n)
	return n, err
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func queryLedger(ctx context.Context, q querier, query string, args ...any) ([]domain.LedgerEntry, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		var created string
		var typ string
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &typ, &e.Amount, &created); err != nil {
			return nil, err
		}
		e.Type = domain.EntryType(typ)
		e.CreatedAt, err = time.Parse(timeLayout, created)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed")
}

func IsBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "deadlocked") ||
		strings.Contains(msg, "busy")
}

// ErrNotFound is a persistence miss. The service maps it to a domain error.
var ErrNotFound = errors.New("record not found")

func mapNotFound(err error, notFound error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return notFound
	}
	return err
}
