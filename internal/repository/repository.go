package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pravinkanna/wallet-transfer-assignment/internal/domain"
)

// Repository persists wallets, transfers, and ledger entries in Postgres.
type Repository struct {
	pool *pgxpool.Pool
}

// New returns a Repository that runs its statements on pool.
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Tx runs persistence operations inside one database transaction.
type Tx struct {
	tx pgx.Tx
}

// InTx runs fn in one transaction. It commits if fn returns nil and rolls
// back otherwise.
func (r *Repository) InTx(ctx context.Context, fn func(*Tx) error) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		return fn(&Tx{tx: tx})
	})
}

// foreignKeyViolation is the Postgres error code for a reference to a row
// that does not exist.
const foreignKeyViolation = "23503"

// InsertTransfer stores a new transfer as PENDING, claiming its idempotency
// key. It returns false if a transfer already holds the key; if a transaction
// that holds the key is still running, it first waits for that transaction
// to end. It returns domain.ErrWalletNotFound if either wallet does not exist.
func (t *Tx) InsertTransfer(ctx context.Context, transfer domain.Transfer) (bool, error) {
	var id string
	err := t.tx.QueryRow(ctx, `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, state)
		VALUES ($1, $2, $3, $4, $5, 'PENDING')
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id`,
		transfer.ID, transfer.IdempotencyKey, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount,
	).Scan(&id)
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation:
		return false, domain.ErrWalletNotFound
	case err != nil:
		return false, fmt.Errorf("insert transfer: %w", err)
	}
	return true, nil
}

// FindTransferByKey reads the transfer that holds an idempotency key.
func (t *Tx) FindTransferByKey(ctx context.Context, key string) (domain.Transfer, error) {
	transfer := domain.Transfer{IdempotencyKey: key}
	err := t.tx.QueryRow(ctx, `
		SELECT id, from_wallet_id, to_wallet_id, amount, state
		FROM transfers
		WHERE idempotency_key = $1`, key,
	).Scan(&transfer.ID, &transfer.FromWalletID, &transfer.ToWalletID, &transfer.Amount, &transfer.State)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("find transfer by key: %w", err)
	}
	return transfer, nil
}

// LockWallet locks a wallet's row until the transaction ends and reads it.
// FOR NO KEY UPDATE queues other transfers on the wallet without conflicting
// with the FOR KEY SHARE locks that foreign keys take (design §7).
func (t *Tx) LockWallet(ctx context.Context, walletID string) (domain.Wallet, error) {
	wallet := domain.Wallet{ID: walletID}
	err := t.tx.QueryRow(ctx,
		`SELECT balance FROM wallets WHERE id = $1 FOR NO KEY UPDATE`, walletID,
	).Scan(&wallet.Balance)
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("lock wallet %s: %w", walletID, err)
	}
	return wallet, nil
}

// DebitWallet subtracts amount from a wallet's balance.
func (t *Tx) DebitWallet(ctx context.Context, walletID string, amount int64) error {
	_, err := t.tx.Exec(ctx, `UPDATE wallets SET balance = balance - $2 WHERE id = $1`, walletID, amount)
	if err != nil {
		return fmt.Errorf("debit wallet %s: %w", walletID, err)
	}
	return nil
}

// CreditWallet adds amount to a wallet's balance.
func (t *Tx) CreditWallet(ctx context.Context, walletID string, amount int64) error {
	_, err := t.tx.Exec(ctx, `UPDATE wallets SET balance = balance + $2 WHERE id = $1`, walletID, amount)
	if err != nil {
		return fmt.Errorf("credit wallet %s: %w", walletID, err)
	}
	return nil
}

// InsertLedgerEntry stores one ledger entry.
func (t *Tx) InsertLedgerEntry(ctx context.Context, entry domain.LedgerEntry) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
		VALUES ($1, $2, $3, $4)`,
		entry.TransferID, entry.WalletID, entry.Type, entry.Amount)
	if err != nil {
		return fmt.Errorf("insert ledger entry: %w", err)
	}
	return nil
}

// UpdateTransferState moves a PENDING transfer to state. Like the domain, it
// refuses to move a transfer that is no longer PENDING.
func (t *Tx) UpdateTransferState(ctx context.Context, transferID string, state domain.TransferState) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE transfers SET state = $2 WHERE id = $1 AND state = 'PENDING'`,
		transferID, state)
	if err != nil {
		return fmt.Errorf("update transfer state: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: transfer %s is not PENDING", domain.ErrInvalidTransition, transferID)
	}
	return nil
}
