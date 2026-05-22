package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HarshilJariwala2000/wallet-transfer-assignment/internal/domain"
)

// pgUniqueViolation is the SQLSTATE code Postgres returns for a UNIQUE or
// PRIMARY KEY constraint violation.
const pgUniqueViolation = "23505"

// ErrIdempotencyKeyExists is returned by InsertIdempotency when the key was
// already inserted by a concurrent request that won the race. The service
// reacts by replaying the stored result.
var ErrIdempotencyKeyExists = errors.New("idempotency key already exists")

// Store is the persistence layer. It exposes a transaction runner (WithTx) and
// owns the database connection pool. It contains no business logic.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// WithTx runs fn inside a single database transaction, committing on success
// and rolling back on any error or panic. The transaction boundary is owned
// here, but the service decides what work happens inside it.
func (s *Store) WithTx(ctx context.Context, fn func(*Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once committed

	if err := fn(&Queries{tx: tx}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Queries holds the persistence operations bound to a single transaction.
// Every method is one statement; none makes workflow decisions.
type Queries struct {
	tx pgx.Tx
}

// IdempotencyRecord is the stored link from an idempotency key to its transfer.
type IdempotencyRecord struct {
	TransferID  string
	RequestHash string
}

// GetIdempotency returns the record for key, or nil if the key is unused.
func (q *Queries) GetIdempotency(ctx context.Context, key string) (*IdempotencyRecord, error) {
	const sql = `SELECT transfer_id, request_hash FROM idempotency_records WHERE idempotency_key = $1`
	var r IdempotencyRecord
	err := q.tx.QueryRow(ctx, sql, key).Scan(&r.TransferID, &r.RequestHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetWalletForUpdate reads a wallet and holds a row-level exclusive lock
// (SELECT ... FOR UPDATE) until the surrounding transaction ends.
func (q *Queries) GetWalletForUpdate(ctx context.Context, id string) (domain.Wallet, error) {
	const sql = `SELECT id, balance, currency FROM wallets WHERE id = $1 FOR UPDATE`
	var w domain.Wallet
	err := q.tx.QueryRow(ctx, sql, id).Scan(&w.ID, &w.Balance, &w.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Wallet{}, domain.ErrWalletNotFound
	}
	return w, err
}

// GetTransfer reads a transfer by ID.
func (q *Queries) GetTransfer(ctx context.Context, id string) (domain.Transfer, error) {
	const sql = `SELECT id, from_wallet_id, to_wallet_id, amount, status FROM transfers WHERE id = $1`
	var t domain.Transfer
	var status string
	err := q.tx.QueryRow(ctx, sql, id).Scan(&t.ID, &t.FromWalletID, &t.ToWalletID, &t.Amount, &status)
	t.Status = domain.TransferStatus(status)
	return t, err
}

// InsertTransfer persists a new transfer row.
func (q *Queries) InsertTransfer(ctx context.Context, t domain.Transfer) error {
	const sql = `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status)
	             VALUES ($1, $2, $3, $4, $5)`
	_, err := q.tx.Exec(ctx, sql, t.ID, t.FromWalletID, t.ToWalletID, t.Amount, string(t.Status))
	return err
}

// InsertLedgerEntry persists one side of a double-entry record.
func (q *Queries) InsertLedgerEntry(ctx context.Context, e domain.LedgerEntry) error {
	const sql = `INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount)
	             VALUES ($1, $2, $3, $4, $5)`
	_, err := q.tx.Exec(ctx, sql, e.ID, e.TransferID, e.WalletID, string(e.Type), e.Amount)
	return err
}

// UpdateWalletBalance writes a new balance for a wallet.
func (q *Queries) UpdateWalletBalance(ctx context.Context, id string, balance int64) error {
	const sql = `UPDATE wallets SET balance = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	_, err := q.tx.Exec(ctx, sql, balance, id)
	return err
}

// UpdateTransferStatus writes a new status for a transfer.
func (q *Queries) UpdateTransferStatus(ctx context.Context, id string, status domain.TransferStatus) error {
	const sql = `UPDATE transfers SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	_, err := q.tx.Exec(ctx, sql, string(status), id)
	return err
}

// InsertIdempotency persists the idempotency key. A unique violation means a
// concurrent request already inserted it; that is reported as
// ErrIdempotencyKeyExists so the service can replay the stored result.
func (q *Queries) InsertIdempotency(ctx context.Context, key, transferID, requestHash string) error {
	const sql = `INSERT INTO idempotency_records (idempotency_key, transfer_id, request_hash)
	             VALUES ($1, $2, $3)`
	_, err := q.tx.Exec(ctx, sql, key, transferID, requestHash)
	if isUniqueViolation(err) {
		return ErrIdempotencyKeyExists
	}
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}
