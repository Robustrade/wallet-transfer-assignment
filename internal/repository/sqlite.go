package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mshabab1296/wallet-transfer-assignment/internal/domain"
	_ "modernc.org/sqlite"
)

type SQLiteRepository struct {
	db      *sql.DB
	writeMu sync.Mutex
}

func OpenSQLite(dsn string) (*SQLiteRepository, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	for _, pragma := range []string{"PRAGMA journal_mode = WAL", "PRAGMA foreign_keys = ON", "PRAGMA busy_timeout = 5000"} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	r := &SQLiteRepository{db: db}
	if err := r.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return r, nil
}
func (r *SQLiteRepository) Close() error { return r.db.Close() }
func (r *SQLiteRepository) migrate(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS wallets (id TEXT PRIMARY KEY, balance INTEGER NOT NULL CHECK (balance >= 0));
CREATE TABLE IF NOT EXISTS transfers (
 id TEXT PRIMARY KEY, idempotency_key TEXT NOT NULL UNIQUE, from_wallet_id TEXT NOT NULL, to_wallet_id TEXT NOT NULL,
 amount INTEGER NOT NULL CHECK (amount > 0), status TEXT NOT NULL CHECK (status IN ('PENDING','PROCESSED','FAILED')),
 failure_reason TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS ledger_entries (
 id INTEGER PRIMARY KEY AUTOINCREMENT, transfer_id TEXT NOT NULL REFERENCES transfers(id), wallet_id TEXT NOT NULL REFERENCES wallets(id),
 entry_type TEXT NOT NULL CHECK (entry_type IN ('DEBIT','CREDIT')), amount INTEGER NOT NULL CHECK (amount > 0), created_at TEXT NOT NULL,
 UNIQUE (transfer_id, entry_type));
CREATE TABLE IF NOT EXISTS idempotency_records (
 idempotency_key TEXT PRIMARY KEY, request_fingerprint TEXT NOT NULL, transfer_id TEXT NOT NULL UNIQUE REFERENCES transfers(id), created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS ledger_entries_transfer_id_idx ON ledger_entries(transfer_id);`)
	return err
}
func (r *SQLiteRepository) CreateWallet(ctx context.Context, id string, balance int64) error {
	if id == "" || balance < 0 {
		return fmt.Errorf("wallet id and non-negative balance are required")
	}
	_, err := r.db.ExecContext(ctx, "INSERT INTO wallets(id, balance) VALUES(?, ?)", id, balance)
	return err
}
func (r *SQLiteRepository) Balance(ctx context.Context, id string) (int64, error) {
	var balance int64
	err := r.db.QueryRowContext(ctx, "SELECT balance FROM wallets WHERE id = ?", id).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, domain.ErrWalletNotFound
	}
	return balance, err
}

// CreateTransfer executes the full state transition in one SQLite write transaction.
func (r *SQLiteRepository) CreateTransfer(ctx context.Context, req domain.CreateTransferRequest) (domain.Transfer, error) {
	// SQLite permits one writer. Serializing requests in this process avoids returning
	// transient SQLITE_BUSY errors; BEGIN IMMEDIATE remains the cross-process guard.
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return domain.Transfer{}, err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return domain.Transfer{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	fingerprint := requestFingerprint(req)
	if existing, found, err := getByKey(ctx, conn, req.IdempotencyKey); err != nil {
		return domain.Transfer{}, err
	} else if found {
		if existing.fingerprint != fingerprint {
			return domain.Transfer{}, domain.ErrIdempotencyConflict
		}
		existing.transfer.Replayed = true
		if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
			return domain.Transfer{}, err
		}
		committed = true
		if existing.transfer.Status == domain.Failed {
			return existing.transfer, domain.ErrInsufficientFunds
		}
		return existing.transfer, nil
	}
	now, id := time.Now().UTC().Format(time.RFC3339Nano), fmt.Sprintf("tr_%d", time.Now().UnixNano())
	transfer := domain.Transfer{ID: id, IdempotencyKey: req.IdempotencyKey, FromWalletID: req.FromWalletID, ToWalletID: req.ToWalletID, Amount: req.Amount, Status: domain.Pending}
	if _, err := conn.ExecContext(ctx, `INSERT INTO transfers(id,idempotency_key,from_wallet_id,to_wallet_id,amount,status,created_at) VALUES(?,?,?,?,?,?,?)`, id, req.IdempotencyKey, req.FromWalletID, req.ToWalletID, req.Amount, domain.Pending, now); err != nil {
		return domain.Transfer{}, err
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO idempotency_records(idempotency_key,request_fingerprint,transfer_id,created_at) VALUES(?,?,?,?)`, req.IdempotencyKey, fingerprint, id, now); err != nil {
		return domain.Transfer{}, err
	}
	var fromBalance, toBalance int64
	if err := conn.QueryRowContext(ctx, "SELECT balance FROM wallets WHERE id = ?", req.FromWalletID).Scan(&fromBalance); errors.Is(err, sql.ErrNoRows) {
		return domain.Transfer{}, domain.ErrWalletNotFound
	} else if err != nil {
		return domain.Transfer{}, err
	}
	if err := conn.QueryRowContext(ctx, "SELECT balance FROM wallets WHERE id = ?", req.ToWalletID).Scan(&toBalance); errors.Is(err, sql.ErrNoRows) {
		return domain.Transfer{}, domain.ErrWalletNotFound
	} else if err != nil {
		return domain.Transfer{}, err
	}
	if fromBalance < req.Amount {
		transfer.Status, transfer.FailureReason = domain.Failed, domain.ErrInsufficientFunds.Error()
		if _, err := conn.ExecContext(ctx, "UPDATE transfers SET status=?, failure_reason=? WHERE id=?", transfer.Status, transfer.FailureReason, id); err != nil {
			return domain.Transfer{}, err
		}
		if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
			return domain.Transfer{}, err
		}
		committed = true
		return transfer, domain.ErrInsufficientFunds
	}
	if _, err := conn.ExecContext(ctx, "UPDATE wallets SET balance=balance-? WHERE id=?", req.Amount, req.FromWalletID); err != nil {
		return domain.Transfer{}, err
	}
	if _, err := conn.ExecContext(ctx, "UPDATE wallets SET balance=balance+? WHERE id=?", req.Amount, req.ToWalletID); err != nil {
		return domain.Transfer{}, err
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO ledger_entries(transfer_id,wallet_id,entry_type,amount,created_at) VALUES(?,?,?,?,?),(?,?,?,?,?)`, id, req.FromWalletID, "DEBIT", req.Amount, now, id, req.ToWalletID, "CREDIT", req.Amount, now); err != nil {
		return domain.Transfer{}, err
	}
	transfer.Status = domain.Processed
	if _, err := conn.ExecContext(ctx, "UPDATE transfers SET status=? WHERE id=? AND status=?", transfer.Status, id, domain.Pending); err != nil {
		return domain.Transfer{}, err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return domain.Transfer{}, err
	}
	committed = true
	return transfer, nil
}

type storedTransfer struct {
	transfer    domain.Transfer
	fingerprint string
}

func getByKey(ctx context.Context, conn *sql.Conn, key string) (storedTransfer, bool, error) {
	var s storedTransfer
	err := conn.QueryRowContext(ctx, `SELECT t.id,t.idempotency_key,t.from_wallet_id,t.to_wallet_id,t.amount,t.status,COALESCE(t.failure_reason,''),i.request_fingerprint FROM transfers t JOIN idempotency_records i ON i.transfer_id=t.id WHERE i.idempotency_key=?`, key).Scan(&s.transfer.ID, &s.transfer.IdempotencyKey, &s.transfer.FromWalletID, &s.transfer.ToWalletID, &s.transfer.Amount, &s.transfer.Status, &s.transfer.FailureReason, &s.fingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return storedTransfer{}, false, nil
	}
	return s, err == nil, err
}
func requestFingerprint(r domain.CreateTransferRequest) string {
	sum := sha256.Sum256([]byte(r.FromWalletID + "\\x00" + r.ToWalletID + "\\x00" + fmt.Sprint(r.Amount)))
	return hex.EncodeToString(sum[:])
}
func (r *SQLiteRepository) LedgerEntryCount(ctx context.Context, transferID string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, "SELECT count(*) FROM ledger_entries WHERE transfer_id=?", transferID).Scan(&n)
	return n, err
}
