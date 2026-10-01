package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RamG-Gupta/wallet-transfer-assignment/internal/domain"

	_ "modernc.org/sqlite"
)

const timeLayout = time.RFC3339Nano

type Store struct {
	db *sql.DB
}

func Open(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", withSQLitePragmas(dsn))
	if err != nil {
		return nil, err
	}
	// WAL + busy timeout lets concurrent tests contend instead of failing immediately.
	// Multiple conns are OK; writers still serialise on BEGIN IMMEDIATE.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func withSQLitePragmas(dsn string) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) CreateWallet(ctx context.Context, id string, balance int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO wallets (id, balance, created_at) VALUES (?, ?, ?)`,
		id, balance, at.UTC().Format(timeLayout),
	)
	if err != nil {
		if isUnique(err) {
			return domain.ErrWalletExists
		}
		return err
	}
	return nil
}

func (s *Store) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	return scanWallet(s.db.QueryRowContext(ctx,
		`SELECT id, balance, created_at FROM wallets WHERE id = ?`, id,
	))
}

// WithTx runs fn inside a single SQLite BEGIN IMMEDIATE transaction.
func (s *Store) WithTx(ctx context.Context, fn func(tx *Tx) error) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	if err := fn(&Tx{ctx: ctx, conn: conn}); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	committed = true
	return nil
}

type IdempotencyRecord struct {
	RequestHash string
	TransferID  string
}

type Tx struct {
	ctx  context.Context
	conn *sql.Conn
}

func (t *Tx) ClaimIdempotency(key, requestHash string, now time.Time) (inserted bool, rec IdempotencyRecord, err error) {
	nowStr := now.UTC().Format(timeLayout)
	_, err = t.conn.ExecContext(t.ctx, `
		INSERT INTO idempotency_records (key, request_hash, transfer_id, status, created_at, updated_at)
		VALUES (?, ?, NULL, 'PROCESSING', ?, ?)`,
		key, requestHash, nowStr, nowStr,
	)
	if err == nil {
		return true, IdempotencyRecord{RequestHash: requestHash}, nil
	}
	if !isUnique(err) {
		return false, IdempotencyRecord{}, err
	}
	rec, err = t.GetIdempotency(key)
	return false, rec, err
}

func (t *Tx) GetIdempotency(key string) (IdempotencyRecord, error) {
	var rec IdempotencyRecord
	var transferID sql.NullString
	err := t.conn.QueryRowContext(t.ctx, `
		SELECT request_hash, transfer_id FROM idempotency_records WHERE key = ?`, key,
	).Scan(&rec.RequestHash, &transferID)
	if errors.Is(err, sql.ErrNoRows) {
		return IdempotencyRecord{}, fmt.Errorf("idempotency key %s not found after conflict", key)
	}
	if err != nil {
		return IdempotencyRecord{}, err
	}
	if transferID.Valid {
		rec.TransferID = transferID.String
	}
	return rec, nil
}

func (t *Tx) CompleteIdempotency(key, transferID string, now time.Time) error {
	_, err := t.conn.ExecContext(t.ctx, `
		UPDATE idempotency_records
		SET status = 'COMPLETED', transfer_id = ?, updated_at = ?
		WHERE key = ?`,
		transferID, now.UTC().Format(timeLayout), key,
	)
	return err
}

func (t *Tx) GetWallet(id string) (domain.Wallet, error) {
	return scanWallet(t.conn.QueryRowContext(t.ctx,
		`SELECT id, balance, created_at FROM wallets WHERE id = ?`, id,
	))
}

func (t *Tx) InsertTransfer(tr domain.Transfer) error {
	_, err := t.conn.ExecContext(t.ctx, `
		INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		tr.ID, tr.FromWalletID, tr.ToWalletID, tr.Amount, string(tr.Status), nullIfEmpty(tr.FailureReason),
		tr.CreatedAt.UTC().Format(timeLayout), tr.UpdatedAt.UTC().Format(timeLayout),
	)
	return err
}

func (t *Tx) UpdateTransferStatus(id string, status domain.TransferStatus, failureReason string, at time.Time) error {
	_, err := t.conn.ExecContext(t.ctx, `
		UPDATE transfers SET status = ?, failure_reason = ?, updated_at = ? WHERE id = ?`,
		string(status), nullIfEmpty(failureReason), at.UTC().Format(timeLayout), id,
	)
	return err
}

func (t *Tx) InsertLedger(transferID, walletID string, entryType domain.EntryType, amount int64, at time.Time) error {
	_, err := t.conn.ExecContext(t.ctx, `
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		transferID, walletID, string(entryType), amount, at.UTC().Format(timeLayout),
	)
	return err
}

func (t *Tx) UpdateBalance(walletID string, newBalance int64) error {
	res, err := t.conn.ExecContext(t.ctx,
		`UPDATE wallets SET balance = ? WHERE id = ?`, newBalance, walletID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("%w: wallet %s", domain.ErrNotFound, walletID)
	}
	return nil
}

func (t *Tx) GetTransfer(id string) (domain.Transfer, error) {
	var tr domain.Transfer
	var failure sql.NullString
	var created, updated string
	err := t.conn.QueryRowContext(t.ctx, `
		SELECT id, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers WHERE id = ?`, id,
	).Scan(&tr.ID, &tr.FromWalletID, &tr.ToWalletID, &tr.Amount, &tr.Status, &failure, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Transfer{}, fmt.Errorf("%w: transfer %s", domain.ErrNotFound, id)
	}
	if err != nil {
		return domain.Transfer{}, err
	}
	if failure.Valid {
		tr.FailureReason = failure.String
	}
	tr.CreatedAt, err = time.Parse(timeLayout, created)
	if err != nil {
		return domain.Transfer{}, err
	}
	tr.UpdatedAt, err = time.Parse(timeLayout, updated)
	if err != nil {
		return domain.Transfer{}, err
	}
	tr.Ledger, err = t.listLedger(id)
	return tr, err
}

func (t *Tx) listLedger(transferID string) ([]domain.LedgerEntry, error) {
	rows, err := t.conn.QueryContext(t.ctx, `
		SELECT wallet_id, entry_type, amount
		FROM ledger_entries WHERE transfer_id = ? ORDER BY id`, transferID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.WalletID, &e.Type, &e.Amount); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []domain.LedgerEntry{}
	}
	return entries, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanWallet(row rowScanner) (domain.Wallet, error) {
	var w domain.Wallet
	var created string
	err := row.Scan(&w.ID, &w.Balance, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Wallet{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Wallet{}, err
	}
	w.CreatedAt, err = time.Parse(timeLayout, created)
	return w, err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func isUnique(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
