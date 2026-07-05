// Package postgres implements repository.TxStore against PostgreSQL using
// database/sql. Register a driver in main.go (this code targets pgx via
// `_ "github.com/jackc/pgx/v5/stdlib"`, opened as sql.Open("pgx", dsn)).
//
// Concurrency strategy: GetWalletForUpdate issues `SELECT ... FOR UPDATE`,
// taking a row-level lock that's held for the rest of the transaction. Two
// transfers racing to debit the same wallet will have the second block on
// that SELECT until the first commits or rolls back — no lost updates, no
// double-spending. Locks are always acquired in ascending wallet-ID order
// (see service.lockBothWallets) to prevent deadlocks between opposite-
// direction transfers over the same wallet pair.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/example/wallet-transfer/internal/domain"
	"github.com/example/wallet-transfer/internal/repository"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) BeginTx(ctx context.Context) (repository.Tx, error) {
	// ReadCommitted is enough here because correctness comes from explicit
	// row locks (FOR UPDATE), not from the isolation level.
	sqlTx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	return &tx{sqlTx: sqlTx}, nil
}

type tx struct {
	sqlTx *sql.Tx
}

func (t *tx) GetWalletForUpdate(ctx context.Context, walletID string) (domain.Wallet, error) {
	var w domain.Wallet
	row := t.sqlTx.QueryRowContext(ctx,
		`SELECT id, balance, version, created_at, updated_at FROM wallets WHERE id = $1 FOR UPDATE`,
		walletID,
	)
	if err := row.Scan(&w.ID, &w.Balance, &w.Version, &w.CreatedAt, &w.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Wallet{}, domain.ErrWalletNotFound
		}
		return domain.Wallet{}, err
	}
	return w, nil
}

func (t *tx) UpdateWalletBalance(ctx context.Context, walletID string, newBalance int64) error {
	res, err := t.sqlTx.ExecContext(ctx,
		`UPDATE wallets SET balance = $1, version = version + 1, updated_at = now() WHERE id = $2`,
		newBalance, walletID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}

func (t *tx) InsertTransfer(ctx context.Context, tr domain.Transfer) error {
	_, err := t.sqlTx.ExecContext(ctx,
		`INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, now(), now())`,
		tr.ID, tr.IdempotencyKey, tr.FromWalletID, tr.ToWalletID, tr.Amount, tr.State,
	)
	return err
}

func (t *tx) UpdateTransferState(ctx context.Context, transferID string, state domain.TransferState, failureReason string) error {
	_, err := t.sqlTx.ExecContext(ctx,
		`UPDATE transfers SET state = $1, failure_reason = $2, updated_at = now() WHERE id = $3`,
		state, nullableString(failureReason), transferID,
	)
	return err
}

func (t *tx) InsertLedgerEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	// A handful of rows per transfer (always exactly 2 today) — plain
	// sequential inserts inside the open transaction are simpler than a
	// batch/COPY path and plenty fast at this volume.
	for _, e := range entries {
		if _, err := t.sqlTx.ExecContext(ctx,
			`INSERT INTO ledger_entries (id, wallet_id, transfer_id, type, amount, created_at)
			 VALUES ($1, $2, $3, $4, $5, now())`,
			e.ID, e.WalletID, e.TransferID, e.Type, e.Amount,
		); err != nil {
			return err
		}
	}
	return nil
}

func (t *tx) ClaimIdempotencyKey(ctx context.Context, key, requestHash string) (bool, error) {
	// Postgres aborts the *entire* transaction after any statement error,
	// including a unique_violation, until a ROLLBACK (or rollback to a
	// savepoint) happens — so without the savepoint here, a losing claim
	// would poison the transaction and the subsequent GetIdempotencyRecord
	// call would fail with "current transaction is aborted". The savepoint
	// scopes the failure to just this one INSERT.
	if _, err := t.sqlTx.ExecContext(ctx, `SAVEPOINT idempotency_claim`); err != nil {
		return false, err
	}

	_, err := t.sqlTx.ExecContext(ctx,
		`INSERT INTO idempotency_records (idempotency_key, request_hash, status, created_at)
		 VALUES ($1, $2, $3, now())`,
		key, requestHash, repository.IdempotencyInProgress,
	)
	if err != nil {
		if isUniqueViolation(err) {
			// Someone else (or an earlier attempt) already owns this key.
			// Roll back to the savepoint to restore the transaction to a
			// usable state before the caller reads the existing record.
			if _, rbErr := t.sqlTx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT idempotency_claim`); rbErr != nil {
				return false, rbErr
			}
			return false, nil
		}
		return false, err
	}

	if _, err := t.sqlTx.ExecContext(ctx, `RELEASE SAVEPOINT idempotency_claim`); err != nil {
		return false, err
	}
	return true, nil
}

func (t *tx) GetIdempotencyRecord(ctx context.Context, key string) (repository.IdempotencyRecord, error) {
	var rec repository.IdempotencyRecord
	var transferID, body sql.NullString
	row := t.sqlTx.QueryRowContext(ctx,
		`SELECT idempotency_key, request_hash, status, transfer_id, response_body
		 FROM idempotency_records WHERE idempotency_key = $1`,
		key,
	)
	if err := row.Scan(&rec.Key, &rec.RequestHash, &rec.Status, &transferID, &body); err != nil {
		return repository.IdempotencyRecord{}, err
	}
	rec.TransferID = transferID.String
	rec.ResponseBody = []byte(body.String)
	return rec, nil
}

func (t *tx) CompleteIdempotencyKey(ctx context.Context, key string, status repository.IdempotencyStatus, transferID string, responseBody []byte) error {
	_, err := t.sqlTx.ExecContext(ctx,
		`UPDATE idempotency_records SET status = $1, transfer_id = $2, response_body = $3 WHERE idempotency_key = $4`,
		status, transferID, string(responseBody), key,
	)
	return err
}

func (t *tx) Commit() error   { return t.sqlTx.Commit() }
func (t *tx) Rollback() error { return t.sqlTx.Rollback() }

func nullableString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// isUniqueViolation detects Postgres' unique_violation (SQLSTATE 23505).
// Matched via error text rather than errors.As(*pgconn.PgError) so this file
// has zero direct pgx import — swap in the typed check if you commit to pgx
// as the permanent driver.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key")
}
