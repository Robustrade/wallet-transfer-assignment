package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/codejk-crypto/wallet-transfer-assignment/internal/domain"
)

type Postgres struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

func (p *Postgres) Begin(ctx context.Context) (pgx.Tx, error) {
	return p.pool.Begin(ctx)
}

type IdempotencyRecord struct {
	Key        string
	Hash       string
	Status     string
	TransferID *string
	HTTPStatus *int
}

func (p *Postgres) InsertWallet(ctx context.Context, w domain.Wallet) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO wallets (id, balance) VALUES ($1, $2)`, w.ID, w.Balance)
	if isUniqueViolation(err) {
		return domain.ErrWalletExists
	}
	return err
}

func (p *Postgres) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	var w domain.Wallet
	err := p.pool.QueryRow(ctx, `SELECT id, balance FROM wallets WHERE id = $1`, id).Scan(&w.ID, &w.Balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Wallet{}, domain.ErrWalletNotFound
	}
	return w, err
}

func ClaimIdempotency(ctx context.Context, tx pgx.Tx, key, requestHash string) (claimed bool, rec IdempotencyRecord, err error) {
	err = tx.QueryRow(ctx, `
		INSERT INTO idempotency_records (idempotency_key, request_hash, status)
		VALUES ($1, $2, $3)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING idempotency_key, request_hash, status, transfer_id, http_status
	`, key, requestHash, domain.IdempotencyInProgress).Scan(&rec.Key, &rec.Hash, &rec.Status, &rec.TransferID, &rec.HTTPStatus)
	if err == nil {
		return true, rec, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, IdempotencyRecord{}, err
	}

	err = tx.QueryRow(ctx, `
		SELECT idempotency_key, request_hash, status, transfer_id, http_status
		FROM idempotency_records
		WHERE idempotency_key = $1
		FOR UPDATE
	`, key).Scan(&rec.Key, &rec.Hash, &rec.Status, &rec.TransferID, &rec.HTTPStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, IdempotencyRecord{}, domain.ErrIdempotencyRetry
	}
	if err != nil {
		return false, IdempotencyRecord{}, err
	}
	return false, rec, nil
}

func CompleteIdempotency(ctx context.Context, tx pgx.Tx, key, transferID string, httpStatus int) error {
	tag, err := tx.Exec(ctx, `
		UPDATE idempotency_records
		SET status = $1, transfer_id = $2, http_status = $3, updated_at = now()
		WHERE idempotency_key = $4 AND status = $5
	`, domain.IdempotencyCompleted, transferID, httpStatus, key, domain.IdempotencyInProgress)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("idempotency complete: expected 1 row, got %d", tag.RowsAffected())
	}
	return nil
}

func LockWallets(ctx context.Context, tx pgx.Tx, firstID, secondID string) (first, second domain.Wallet, err error) {
	rows, err := tx.Query(ctx, `
		SELECT id, balance FROM wallets
		WHERE id = $1 OR id = $2
		ORDER BY id
		FOR UPDATE
	`, firstID, secondID)
	if err != nil {
		return domain.Wallet{}, domain.Wallet{}, err
	}
	defer rows.Close()

	var wallets []domain.Wallet
	for rows.Next() {
		var w domain.Wallet
		if err := rows.Scan(&w.ID, &w.Balance); err != nil {
			return domain.Wallet{}, domain.Wallet{}, err
		}
		wallets = append(wallets, w)
	}
	if err := rows.Err(); err != nil {
		return domain.Wallet{}, domain.Wallet{}, err
	}
	if len(wallets) != 2 {
		return domain.Wallet{}, domain.Wallet{}, domain.ErrWalletNotFound
	}
	return wallets[0], wallets[1], nil
}

func InsertTransfer(ctx context.Context, tx pgx.Tx, t domain.Transfer) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, failure_reason)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))
	`, t.ID, t.FromWalletID, t.ToWalletID, t.Amount, t.Status, t.FailureReason)
	return err
}

func UpdateTransferStatus(ctx context.Context, tx pgx.Tx, id, fromStatus, toStatus, failureReason string) error {
	if err := domain.CanTransition(fromStatus, toStatus); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE transfers
		SET status = $1, failure_reason = NULLIF($2, ''), updated_at = now()
		WHERE id = $3 AND status = $4
	`, toStatus, failureReason, id, fromStatus)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrInvalidStateChange
	}
	return nil
}

func InsertLedgerEntries(ctx context.Context, tx pgx.Tx, debit, credit domain.LedgerEntry) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount)
		VALUES ($1, $2, $3, $4, $5), ($6, $7, $8, $9, $10)
	`, debit.ID, debit.TransferID, debit.WalletID, debit.Type, debit.Amount,
		credit.ID, credit.TransferID, credit.WalletID, credit.Type, credit.Amount)
	return err
}

func ApplyBalances(ctx context.Context, tx pgx.Tx, fromID, toID string, amount int64) error {
	tag, err := tx.Exec(ctx, `
		UPDATE wallets SET balance = balance - $1 WHERE id = $2 AND balance >= $1
	`, amount, fromID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("debit wallet %s: no row updated", fromID)
	}
	tag, err = tx.Exec(ctx, `UPDATE wallets SET balance = balance + $1 WHERE id = $2`, amount, toID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("credit wallet %s: no row updated", toID)
	}
	return nil
}

func GetTransfer(ctx context.Context, tx pgx.Tx, id string) (domain.Transfer, error) {
	var t domain.Transfer
	err := tx.QueryRow(ctx, `
		SELECT id, from_wallet_id, to_wallet_id, amount, status, COALESCE(failure_reason, '')
		FROM transfers WHERE id = $1
	`, id).Scan(&t.ID, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status, &t.FailureReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Transfer{}, fmt.Errorf("transfer %s not found", id)
	}
	return t, err
}

func ListLedgerByTransfer(ctx context.Context, pool *pgxpool.Pool, transferID string) ([]domain.LedgerEntry, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, transfer_id, wallet_id, entry_type, amount
		FROM ledger_entries WHERE transfer_id = $1 ORDER BY entry_type
	`, transferID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.LedgerEntry, error) {
		var e domain.LedgerEntry
		err := row.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.Type, &e.Amount)
		return e, err
	})
}

func CountTransfers(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var n int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM transfers`).Scan(&n)
	return n, err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
