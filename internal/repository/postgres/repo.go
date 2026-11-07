package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/celinagupta/wallet-transfer-assignment/internal/domain"
	"github.com/celinagupta/wallet-transfer-assignment/internal/repository"
)

// querier is satisfied by pgx.Tx; repo methods only ever run inside a transaction.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type repo struct {
	q querier
}

var _ repository.Repository = (*repo)(nil)

// LockWallets takes FOR NO KEY UPDATE locks in ascending id order. The sort runs
// below the row-locking step, so concurrent callers always lock in the same order
// and cannot deadlock on each other. NO KEY UPDATE (rather than UPDATE) does not
// conflict with the FOR KEY SHARE locks that foreign-key checks take.
func (r *repo) LockWallets(ctx context.Context, ids ...string) ([]domain.Wallet, error) {
	rows, err := r.q.Query(ctx,
		`SELECT id, balance FROM wallets WHERE id = ANY($1) ORDER BY id FOR NO KEY UPDATE`, ids)
	if err != nil {
		return nil, fmt.Errorf("lock wallets: %w", err)
	}
	wallets, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Wallet, error) {
		var w domain.Wallet
		err := row.Scan(&w.ID, &w.Balance)
		return w, err
	})
	if err != nil {
		return nil, fmt.Errorf("lock wallets: %w", err)
	}
	return wallets, nil
}

func (r *repo) InsertTransfer(ctx context.Context, t *domain.Transfer) (bool, error) {
	err := r.q.QueryRow(ctx, `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING created_at`,
		t.ID, nullIfEmpty(t.IdempotencyKey), t.FromWalletID, t.ToWalletID, t.Amount,
		string(t.Status), nullIfEmpty(string(t.FailureReason)),
	).Scan(&t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // key already taken
	}
	if err != nil {
		return false, fmt.Errorf("insert transfer: %w", err)
	}
	return true, nil
}

func (r *repo) TransferByIdempotencyKey(ctx context.Context, key string) (domain.Transfer, error) {
	var (
		t       domain.Transfer
		idemKey *string
		reason  *string
	)
	err := r.q.QueryRow(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at
		FROM transfers WHERE idempotency_key = $1`, key,
	).Scan(&t.ID, &idemKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status, &reason, &t.CreatedAt)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("get transfer by idempotency key: %w", err)
	}
	if idemKey != nil {
		t.IdempotencyKey = *idemKey
	}
	if reason != nil {
		t.FailureReason = domain.FailureReason(*reason)
	}
	return t, nil
}

func (r *repo) UpdateTransferStatus(ctx context.Context, t domain.Transfer) error {
	tag, err := r.q.Exec(ctx, `
		UPDATE transfers SET status = $2, failure_reason = $3, updated_at = now()
		WHERE id = $1 AND status = 'PENDING'`,
		t.ID, string(t.Status), nullIfEmpty(string(t.FailureReason)))
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("update transfer %s to %s: %w", t.ID, t.Status, domain.ErrInvalidTransition)
	}
	return nil
}

func (r *repo) AddToBalance(ctx context.Context, walletID string, delta int64) error {
	tag, err := r.q.Exec(ctx,
		`UPDATE wallets SET balance = balance + $2, updated_at = now() WHERE id = $1`, walletID, delta)
	if err != nil {
		return fmt.Errorf("update balance of %s: %w", walletID, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("update balance of %s: %w", walletID, domain.ErrWalletNotFound)
	}
	return nil
}

func (r *repo) InsertLedgerEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	for _, e := range entries {
		if _, err := r.q.Exec(ctx, `
			INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount) VALUES ($1, $2, $3, $4)`,
			e.TransferID, e.WalletID, string(e.Type), e.Amount,
		); err != nil {
			return fmt.Errorf("insert %s ledger entry for %s: %w", e.Type, e.WalletID, err)
		}
	}
	return nil
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
