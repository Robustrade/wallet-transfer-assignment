// Package postgres implements the repository ports with PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

// Store runs wallet and transfer SQL.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a store that uses an existing pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// NewPool opens a pool sized for concurrent transfer requests.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 20
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return pool, nil
}

// WithTx runs fn in one READ COMMITTED transaction.
// A non-nil error rolls the transaction back, including any idempotency claim.
func (s *Store) WithTx(ctx context.Context, fn func(tx repository.Tx) error) error {
	pgTx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer rollback(pgTx)

	if err := fn(&tx{tx: pgTx}); err != nil {
		return err
	}
	if err := pgTx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func rollback(pgTx pgx.Tx) {
	// The request context may already be cancelled. Roll back on a fresh context.
	rbCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = pgTx.Rollback(rbCtx)
}

type tx struct {
	tx pgx.Tx
}

func (t *tx) ClaimIdempotency(ctx context.Context, key, requestHash string) (repository.IdempotencyRecord, bool, error) {
	row := t.tx.QueryRow(ctx, `
		INSERT INTO idempotency_records (idempotency_key, request_hash)
		VALUES ($1, $2)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING idempotency_key, request_hash, transfer_id::text, response_status, response_body
	`, key, requestHash)
	rec, err := scanIdempotency(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.IdempotencyRecord{}, false, nil
	}
	if err != nil {
		return repository.IdempotencyRecord{}, false, err
	}
	return rec, true, nil
}

func (t *tx) LockIdempotency(ctx context.Context, key string) (repository.IdempotencyRecord, bool, error) {
	row := t.tx.QueryRow(ctx, `
		SELECT idempotency_key, request_hash, transfer_id::text, response_status, response_body
		FROM idempotency_records
		WHERE idempotency_key = $1
		FOR UPDATE
	`, key)
	rec, err := scanIdempotency(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.IdempotencyRecord{}, false, nil
	}
	if err != nil {
		return repository.IdempotencyRecord{}, false, err
	}
	return rec, true, nil
}

func (t *tx) SaveIdempotencyResult(ctx context.Context, key string, status int, body []byte, transferID string) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE idempotency_records
		SET response_status = $2,
		    response_body = $3,
		    transfer_id = $4::uuid
		WHERE idempotency_key = $1
		  AND response_status IS NULL
	`, key, status, string(body), transferID)
	if err != nil {
		return fmt.Errorf("save idempotency result: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("save idempotency result for %s affected %d rows", key, tag.RowsAffected())
	}
	return nil
}

func (t *tx) LockWallets(ctx context.Context, ids ...string) (map[string]domain.Wallet, error) {
	// Lock one row at a time in sorted id order.
	// A single SELECT ... ORDER BY id FOR UPDATE can lock rows before it sorts them,
	// so two opposing transfers could still deadlock. Separate statements do not.
	ordered := append([]string(nil), ids...)
	sort.Strings(ordered)

	wallets := make(map[string]domain.Wallet, len(ordered))
	var previous string
	for _, id := range ordered {
		if id == previous {
			continue
		}
		previous = id
		var wallet domain.Wallet
		err := t.tx.QueryRow(ctx, `
			SELECT id, balance, initial_balance
			FROM wallets
			WHERE id = $1
			FOR UPDATE
		`, id).Scan(&wallet.ID, &wallet.Balance, &wallet.InitialBalance)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("lock wallet: %w", err)
		}
		wallets[wallet.ID] = wallet
	}
	return wallets, nil
}

func (t *tx) InsertTransfer(ctx context.Context, transfer domain.Transfer) (string, error) {
	var id string
	err := t.tx.QueryRow(ctx, `
		INSERT INTO transfers (
			id, idempotency_key, from_wallet_id, to_wallet_id, amount, status
		) VALUES (
			gen_random_uuid(), $1, $2, $3, $4, $5
		)
		RETURNING id::text
	`, transfer.IdempotencyKey, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount, string(transfer.Status)).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert transfer: %w", err)
	}
	return id, nil
}

func (t *tx) UpdateTransferStatus(ctx context.Context, id string, from, to domain.Status, reason string) error {
	if !domain.CanTransition(from, to) {
		return fmt.Errorf("illegal transfer transition %s to %s", from, to)
	}
	var reasonArg any
	if reason != "" {
		reasonArg = reason
	}
	tag, err := t.tx.Exec(ctx, `
		UPDATE transfers
		SET status = $3,
		    failure_reason = $4,
		    updated_at = now()
		WHERE id = $1::uuid
		  AND status = $2
	`, id, string(from), string(to), reasonArg)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("transfer %s status update affected %d rows", id, tag.RowsAffected())
	}
	return nil
}

func (t *tx) InsertLedgerPair(ctx context.Context, transferID, fromWalletID, toWalletID string, amount int64) error {
	tag, err := t.tx.Exec(ctx, `
		INSERT INTO ledger_entries (wallet_id, transfer_id, entry_type, amount)
		VALUES
			($1, $3::uuid, 'DEBIT', $4),
			($2, $3::uuid, 'CREDIT', $4)
	`, fromWalletID, toWalletID, transferID, amount)
	if err != nil {
		return fmt.Errorf("insert ledger pair: %w", err)
	}
	if tag.RowsAffected() != 2 {
		return fmt.Errorf("insert ledger pair affected %d rows", tag.RowsAffected())
	}
	return nil
}

func (t *tx) DebitWallet(ctx context.Context, walletID string, amount int64) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE wallets
		SET balance = balance - $2
		WHERE id = $1
		  AND balance >= $2
	`, walletID, amount)
	if err != nil {
		return fmt.Errorf("debit wallet: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrInsufficientFunds
	}
	return nil
}

func (t *tx) CreditWallet(ctx context.Context, walletID string, amount int64) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE wallets
		SET balance = balance + $2
		WHERE id = $1
	`, walletID, amount)
	if err != nil {
		return fmt.Errorf("credit wallet: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("credit wallet %s affected %d rows", walletID, tag.RowsAffected())
	}
	return nil
}

// CreateWallet inserts a wallet whose balance starts at initial_balance.
func (s *Store) CreateWallet(ctx context.Context, wallet domain.Wallet) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO wallets (id, balance, initial_balance)
		VALUES ($1, $2, $3)
	`, wallet.ID, wallet.Balance, wallet.InitialBalance)
	if err != nil {
		return mapWriteErr(err)
	}
	return nil
}

// GetWallet reads one wallet.
func (s *Store) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	var wallet domain.Wallet
	err := s.pool.QueryRow(ctx, `
		SELECT id, balance, initial_balance
		FROM wallets
		WHERE id = $1
	`, id).Scan(&wallet.ID, &wallet.Balance, &wallet.InitialBalance)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Wallet{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("get wallet: %w", err)
	}
	return wallet, nil
}

// GetTransfer reads one transfer and its ledger entries.
func (s *Store) GetTransfer(ctx context.Context, id string) (domain.Transfer, []domain.LedgerEntry, error) {
	var transfer domain.Transfer
	var reason *string
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason
		FROM transfers
		WHERE id = $1::uuid
	`, id).Scan(
		&transfer.ID,
		&transfer.IdempotencyKey,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&transfer.Status,
		&reason,
	)
	if errors.Is(err, pgx.ErrNoRows) || isInvalidText(err) {
		return domain.Transfer{}, nil, domain.ErrNotFound
	}
	if err != nil {
		return domain.Transfer{}, nil, fmt.Errorf("get transfer: %w", err)
	}
	if reason != nil {
		transfer.FailureReason = *reason
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, wallet_id, transfer_id::text, entry_type, amount
		FROM ledger_entries
		WHERE transfer_id = $1::uuid
		ORDER BY entry_type
	`, id)
	if err != nil {
		return domain.Transfer{}, nil, fmt.Errorf("list ledger entries: %w", err)
	}
	defer rows.Close()

	entries := make([]domain.LedgerEntry, 0, 2)
	for rows.Next() {
		var entry domain.LedgerEntry
		if err := rows.Scan(&entry.ID, &entry.WalletID, &entry.TransferID, &entry.Type, &entry.Amount); err != nil {
			return domain.Transfer{}, nil, fmt.Errorf("scan ledger entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return domain.Transfer{}, nil, fmt.Errorf("read ledger entries: %w", err)
	}
	return transfer, entries, nil
}

func scanIdempotency(row pgx.Row) (repository.IdempotencyRecord, error) {
	var rec repository.IdempotencyRecord
	var transferID *string
	var status *int
	var body *string
	err := row.Scan(&rec.Key, &rec.RequestHash, &transferID, &status, &body)
	if err != nil {
		return repository.IdempotencyRecord{}, err
	}
	if transferID != nil {
		rec.TransferID = *transferID
	}
	if status != nil {
		rec.ResponseStatus = *status
	}
	if body != nil {
		rec.ResponseBody = []byte(*body)
	}
	return rec, nil
}

func mapWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domain.ErrAlreadyExists
	}
	return err
}

func isInvalidText(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}
