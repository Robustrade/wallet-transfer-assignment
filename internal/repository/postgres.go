package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lib/pq"

	"wallettransfer/internal/domain"
	"wallettransfer/internal/service"
)

// Postgres wraps database/sql without owning business decisions.
type Postgres struct{ DB *sql.DB }

func NewPostgres(db *sql.DB) *Postgres { return &Postgres{DB: db} }

func (p *Postgres) WithinTransaction(ctx context.Context, fn func(service.Transaction) error) error {
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback() // harmless after successful commit
	if err := fn(&postgresTransaction{tx: tx}); err != nil {
		return translateError(err)
	}
	return translateError(tx.Commit())
}

type postgresTransaction struct{ tx *sql.Tx }

func (p *postgresTransaction) Claim(ctx context.Context, request domain.TransferRequest) (string, *domain.TransferResult, error) {
	id, err := newUUID()
	if err != nil {
		return "", nil, err
	}
	const insert = `INSERT INTO transfers
        (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status)
        VALUES ($1, $2, $3, $4, $5, 'PENDING')
        ON CONFLICT (idempotency_key) DO NOTHING
        RETURNING id::text`
	var claimedID string
	err = p.tx.QueryRowContext(ctx, insert, id, request.IdempotencyKey,
		request.FromWalletID, request.ToWalletID, request.Amount).Scan(&claimedID)
	if err == nil {
		return claimedID, nil, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", nil, translateError(err)
	}

	// A new statement is essential under READ COMMITTED. The conflicting row
	// could be invisible to the INSERT statement's snapshot until it commits.
	const existing = `SELECT id::text, from_wallet_id, to_wallet_id, amount,
        status, COALESCE(failure_reason, '') FROM transfers WHERE idempotency_key=$1`
	var prior domain.TransferResult
	var original domain.TransferRequest
	if err := p.tx.QueryRowContext(ctx, existing, request.IdempotencyKey).Scan(
		&prior.ID, &original.FromWalletID, &original.ToWalletID,
		&original.Amount, &prior.Status, &prior.FailureReason,
	); err != nil {
		return "", nil, fmt.Errorf("read committed idempotency result: %w", err)
	}
	original.IdempotencyKey = request.IdempotencyKey
	if !request.SameOperation(original) {
		return "", nil, domain.ErrIdempotencyConflict
	}
	if prior.Status != domain.StatusProcessed && prior.Status != domain.StatusFailed {
		return "", nil, fmt.Errorf("committed transfer is not terminal: %w", domain.ErrInvariantViolation)
	}
	return prior.ID, &prior, nil
}

func (p *postgresTransaction) LockWallet(ctx context.Context, id string) (int64, error) {
	var balance int64
	err := p.tx.QueryRowContext(ctx,
		`SELECT balance FROM wallets WHERE id=$1 FOR NO KEY UPDATE`, id).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, domain.ErrWalletNotFound
	}
	return balance, err
}

func (p *postgresTransaction) DebitIfEnough(ctx context.Context, id string, amount int64) (bool, error) {
	result, err := p.tx.ExecContext(ctx,
		`UPDATE wallets SET balance=balance-$1 WHERE id=$2 AND balance >= $1`, amount, id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func (p *postgresTransaction) Credit(ctx context.Context, id string, amount int64) error {
	result, err := p.tx.ExecContext(ctx,
		`UPDATE wallets SET balance=balance+$1 WHERE id=$2`, amount, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("credit affected %d wallets: %w", affected, domain.ErrInvariantViolation)
	}
	return nil
}

func (p *postgresTransaction) PostLedger(ctx context.Context, transferID, fromID, toID string, amount int64) error {
	_, err := p.tx.ExecContext(ctx, `INSERT INTO ledger_entries
        (transfer_id, wallet_id, entry_type, amount) VALUES
        ($1,$2,'DEBIT',$4), ($1,$3,'CREDIT',$4)`, transferID, fromID, toID, amount)
	return err
}

func (p *postgresTransaction) Finish(ctx context.Context, id, status, reason string) error {
	result, err := p.tx.ExecContext(ctx, `UPDATE transfers
        SET status=$2, failure_reason=NULLIF($3, '')
        WHERE id=$1 AND status='PENDING'`, id, status, reason)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("transfer finalization affected %d rows: %w", affected, domain.ErrInvariantViolation)
	}
	return nil
}

func translateError(err error) error {
	if err == nil {
		return nil
	}
	// 40P01 (deadlock) and 40001 (serialization failure) certify that
	// PostgreSQL aborted this transaction. Retry only the *whole* transaction.
	// Other errors, especially an unknown COMMIT outcome, are not automatically
	// retried here: clients may safely use the original idempotency key.
	if errors.Is(err, domain.ErrRetryableTransaction) {
		return err
	}
	var postgresErr *pq.Error
	if errors.As(err, &postgresErr) {
		switch string(postgresErr.Code) {
		case "40P01", "40001":
			return fmt.Errorf("%w: %w", domain.ErrRetryableTransaction, err)
		case "23503":
			// A missing wallet on the initial transfer claim is a client 404.
			// A foreign-key failure elsewhere (for example, a ledger-to-transfer
			// reference) indicates an internal bug, not a missing request wallet.
			if postgresErr.Table == "transfers" &&
				(postgresErr.Constraint == "transfers_from_wallet_id_fkey" ||
					postgresErr.Constraint == "transfers_to_wallet_id_fkey") {
				return fmt.Errorf("wallet foreign key: %w", domain.ErrWalletNotFound)
			}
		}
	}
	return err
}

// Generate version 4 UUIDs without an additional package or DB extension.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
