// Package repository contains all SQL. Each method takes an already-open
// transaction (DBTX) so the *service* owns the transaction boundary -- the
// entire transfer commits or rolls back as one unit. The repository knows
// tables and columns; it knows nothing about why an operation happens.
package repository

import (
	"context"
	"encoding/json"

	"github.com/example/wallet-transfer/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBTX is satisfied by both pgx.Tx and *pgxpool.Pool, so repository methods are
// agnostic to whether they run inside an explicit transaction (they always do
// here, but the interface keeps the coupling loose).
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// IdempotencyRecord is the stored state of a request keyed by its idempotency key.
type IdempotencyRecord struct {
	Key            string
	RequestHash    string
	TransferID     string
	Status         string // IN_PROGRESS | COMPLETED
	ResponseStatus int
	ResponseBody   []byte
}

const (
	IdempIn   = "IN_PROGRESS"
	IdempDone = "COMPLETED"
)

// Repository is a thin set of SQL operations.
type Repository struct{}

func New() *Repository { return &Repository{} }

// ClaimIdempotencyKey atomically inserts an IN_PROGRESS record for the key.
// It returns claimed=true if this caller won the key, or claimed=false if the
// key already exists (a duplicate request). ON CONFLICT DO NOTHING is used so a
// duplicate does NOT abort the surrounding transaction; a concurrent duplicate
// blocks on the unique index until the original commits, then sees the conflict.
func (r *Repository) ClaimIdempotencyKey(ctx context.Context, tx DBTX, key, hash string) (bool, error) {
	tag, err := tx.Exec(ctx,
		`INSERT INTO idempotency_records (idempotency_key, request_hash, status)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (idempotency_key) DO NOTHING`,
		key, hash, IdempIn)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// GetIdempotencyRecordForUpdate locks and returns the existing record for a key.
// Locking serializes a concurrent duplicate behind the original request.
func (r *Repository) GetIdempotencyRecordForUpdate(ctx context.Context, tx DBTX, key string) (*IdempotencyRecord, error) {
	var rec IdempotencyRecord
	var transferID *string
	var respStatus *int
	var respBody []byte
	err := tx.QueryRow(ctx,
		`SELECT idempotency_key, request_hash, transfer_id::text, status, response_status, response_body
		   FROM idempotency_records
		  WHERE idempotency_key = $1
		  FOR UPDATE`, key).
		Scan(&rec.Key, &rec.RequestHash, &transferID, &rec.Status, &respStatus, &respBody)
	if err != nil {
		return nil, err
	}
	if transferID != nil {
		rec.TransferID = *transferID
	}
	if respStatus != nil {
		rec.ResponseStatus = *respStatus
	}
	rec.ResponseBody = respBody
	return &rec, nil
}

// FinalizeIdempotencyRecord marks the key COMPLETED and stores the response to
// replay for future duplicates.
func (r *Repository) FinalizeIdempotencyRecord(ctx context.Context, tx DBTX, key, transferID string, respStatus int, respBody []byte) error {
	_, err := tx.Exec(ctx,
		`UPDATE idempotency_records
		    SET status = $2, transfer_id = $3, response_status = $4, response_body = $5
		  WHERE idempotency_key = $1`,
		key, IdempDone, transferID, respStatus, json.RawMessage(respBody))
	return err
}

// LockWallets locks the given wallets FOR UPDATE and returns them keyed by id.
// Callers MUST pass ids in a deterministic (sorted) order to avoid deadlocks.
func (r *Repository) LockWallets(ctx context.Context, tx DBTX, ids []string) (map[string]domain.Wallet, error) {
	rows, err := tx.Query(ctx,
		`SELECT id, balance, currency
		   FROM wallets
		  WHERE id = ANY($1)
		  ORDER BY id
		  FOR UPDATE`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]domain.Wallet, len(ids))
	for rows.Next() {
		var w domain.Wallet
		if err := rows.Scan(&w.ID, &w.Balance, &w.Currency); err != nil {
			return nil, err
		}
		out[w.ID] = w
	}
	return out, rows.Err()
}

// InsertTransfer creates a transfer in PENDING state and returns its id.
func (r *Repository) InsertTransfer(ctx context.Context, tx DBTX, from, to string, amount domain.Amount) (string, error) {
	var id string
	err := tx.QueryRow(ctx,
		`INSERT INTO transfers (from_wallet_id, to_wallet_id, amount, status)
		 VALUES ($1, $2, $3, 'PENDING')
		 RETURNING id::text`,
		from, to, amount).Scan(&id)
	return id, err
}

// AdjustBalance applies a signed delta to a wallet's stored balance.
func (r *Repository) AdjustBalance(ctx context.Context, tx DBTX, walletID string, delta domain.Amount) error {
	_, err := tx.Exec(ctx,
		`UPDATE wallets SET balance = balance + $2, updated_at = now() WHERE id = $1`,
		walletID, delta)
	return err
}

// InsertLedgerEntry writes one half of the double-entry record.
func (r *Repository) InsertLedgerEntry(ctx context.Context, tx DBTX, transferID, walletID string, t domain.EntryType, amount domain.Amount) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
		 VALUES ($1, $2, $3, $4)`,
		transferID, walletID, string(t), amount)
	return err
}

// MarkProcessed performs the guarded PENDING -> PROCESSED transition.
// Returns true only if it actually moved a row out of PENDING.
func (r *Repository) MarkProcessed(ctx context.Context, tx DBTX, transferID string) (bool, error) {
	tag, err := tx.Exec(ctx,
		`UPDATE transfers SET status = 'PROCESSED', updated_at = now()
		  WHERE id = $1 AND status = 'PENDING'`, transferID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// MarkFailed performs the guarded PENDING -> FAILED transition.
func (r *Repository) MarkFailed(ctx context.Context, tx DBTX, transferID, reason string) (bool, error) {
	tag, err := tx.Exec(ctx,
		`UPDATE transfers SET status = 'FAILED', failure_reason = $2, updated_at = now()
		  WHERE id = $1 AND status = 'PENDING'`, transferID, reason)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// --- Read helpers (used by tests and optional balance/history APIs) ---

// WalletBalance returns the stored balance for a wallet.
func (r *Repository) WalletBalance(ctx context.Context, db DBTX, walletID string) (domain.Amount, error) {
	var bal domain.Amount
	err := db.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = $1`, walletID).Scan(&bal)
	return bal, err
}

// DerivedBalance computes a wallet's balance from the ledger
// (SUM of credits minus debits) -- used to reconcile against the stored balance.
func (r *Repository) DerivedBalance(ctx context.Context, db DBTX, walletID string) (domain.Amount, error) {
	var bal domain.Amount
	err := db.QueryRow(ctx,
		`SELECT COALESCE(SUM(CASE WHEN type = 'CREDIT' THEN amount ELSE -amount END), 0)
		   FROM ledger_entries WHERE wallet_id = $1`, walletID).Scan(&bal)
	return bal, err
}
