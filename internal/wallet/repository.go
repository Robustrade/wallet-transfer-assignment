package wallet

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type WalletRepository interface {
	Transfer(ctx context.Context, idempotencyKey string, fromID, toID, amount int64) (*Transaction, error)
	CreateWallet(ctx context.Context, walletID int64, balance int64) error
	GetWallet(ctx context.Context, walletID int64) (*Wallet, error)
}

type repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) WalletRepository {
	return &repository{
		db: db,
	}
}

var ErrInsufficientFunds = errors.New("insufficient funds")

func (r *repository) Transfer(ctx context.Context, idempotencyKey string, fromID, toID, amount int64) (*Transaction, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1) Lock wallets
	balances, err := r.lockWallets(ctx, tx, fromID, toID)
	if err != nil {
		return nil, err
	}

	// 2) Validate wallet existence and balance (before creating a PENDING transaction)
	fromBal, ok := balances[fromID]
	if !ok {
		_ = tx.Commit(ctx)
		return nil, errors.New("error fetching sender wallet balance")
	}

	_, ok = balances[toID]
	if !ok {
		_ = tx.Commit(ctx)
		return nil, errors.New("error fetching receiver wallet balance")
	}

	if fromBal < amount {
		_ = tx.Commit(ctx)
		return nil, ErrInsufficientFunds
	}

	// 3) If transaction already exists, return it
	if existing, err := r.fetchExistingTransaction(ctx, tx, idempotencyKey); err == nil {
		_ = tx.Commit(ctx)
		return existing, nil
	} else if err != pgx.ErrNoRows {
		return nil, err
	}

	// 4) Insert pending transaction (handle concurrent unique insert)
	txID, existing, err := r.insertPendingTransaction(ctx, tx, idempotencyKey, fromID, toID, amount)
	if err != nil {
		return nil, err
	}
	if existing {
		// concurrent insert happened; fetch and return
		if t, err := r.fetchExistingTransaction(ctx, tx, idempotencyKey); err != nil {
			return nil, err
		} else {
			_ = tx.Commit(ctx)
			return t, nil
		}
	}

	// 5) Create ledger entries
	if err := r.createLedgerEntries(ctx, tx, txID, fromID, toID, amount); err != nil {
		_ = r.markTransactionState(ctx, tx, txID, StateFailed)
		return nil, err
	}

	// 6) Update balances
	if err := r.updateWalletBalances(ctx, tx, fromID, toID, amount); err != nil {
		_ = r.markTransactionState(ctx, tx, txID, StateFailed)
		return nil, err
	}

	// 7) Mark processed and commit
	if err := r.markTransactionState(ctx, tx, txID, StateProcessed); err != nil {
		_ = r.markTransactionState(ctx, tx, txID, StateFailed)
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// fetch final transaction
	final, err := r.fetchTransactionByID(ctx, txID)
	if err != nil {
		return nil, err
	}
	return final, nil
}

// lockWallets locks both wallet rows in deterministic order and returns balances map
func (r *repository) lockWallets(ctx context.Context, tx pgx.Tx, fromID, toID int64) (map[int64]int64, error) {
	a, b := fromID, toID
	if a > b {
		a, b = b, a
	}
	rows, err := tx.Query(ctx, `SELECT wallet_id, balance FROM wallets WHERE wallet_id IN ($1,$2) FOR UPDATE`, a, b)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	balances := map[int64]int64{}
	for rows.Next() {
		var wid, bal int64
		if err := rows.Scan(&wid, &bal); err != nil {
			return nil, err
		}
		balances[wid] = bal
	}
	return balances, nil
}

// fetchExistingTransaction returns a transaction by idempotency key or pgx.ErrNoRows
func (r *repository) fetchExistingTransaction(ctx context.Context, tx pgx.Tx, idempotencyKey string) (*Transaction, error) {
	var t Transaction
	err := tx.QueryRow(ctx, `SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, created_at, updated_at FROM transactions WHERE idempotency_key=$1`, idempotencyKey).Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.State, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// insertPendingTransaction inserts a transaction in PENDING state. If concurrent unique violation occurs, returns unique=true.
func (r *repository) insertPendingTransaction(ctx context.Context, tx pgx.Tx, idempotencyKey string, fromID, toID, amount int64) (int64, bool, error) {
	var txID int64
	err := tx.QueryRow(ctx, `INSERT INTO transactions (idempotency_key, from_wallet_id, to_wallet_id, amount, state) VALUES ($1,$2,$3,$4,$5) RETURNING id`, idempotencyKey, fromID, toID, amount, StatePending).Scan(&txID)
	if err != nil {
		if pgerr, ok := err.(*pgconn.PgError); ok && pgerr.Code == "23505" {
			return 0, true, nil
		}
		return 0, false, err
	}
	return txID, false, nil
}

// CreateWallet inserts a new wallet row. If wallet exists, it's a no-op.
func (r *repository) CreateWallet(ctx context.Context, walletID int64, balance int64) error {
	_, err := r.db.Exec(ctx, `INSERT INTO wallets (wallet_id, balance) VALUES ($1,$2) ON CONFLICT (wallet_id) DO NOTHING`, walletID, balance)
	return err
}

// GetWallet retrieves wallet by id
func (r *repository) GetWallet(ctx context.Context, walletID int64) (*Wallet, error) {
	var w Wallet
	err := r.db.QueryRow(ctx, `SELECT wallet_id, balance, created_at, updated_at FROM wallets WHERE wallet_id=$1`, walletID).Scan(
		&w.WalletID, &w.Balance, &w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// fetchTransactionByID retrieves a transaction by numeric id (uses connection pool)
func (r *repository) fetchTransactionByID(ctx context.Context, id int64) (*Transaction, error) {
	var t Transaction
	err := r.db.QueryRow(ctx, `SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, created_at, updated_at FROM transactions WHERE id=$1`, id).Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.State, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *repository) createLedgerEntries(ctx context.Context, tx pgx.Tx, txID, fromID, toID, amount int64) error {
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_entries (transaction_id, wallet_id, type, amount) VALUES ($1,$2,'DEBIT',$3)`, txID, fromID, amount); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_entries (transaction_id, wallet_id, type, amount) VALUES ($1,$2,'CREDIT',$3)`, txID, toID, amount); err != nil {
		return err
	}
	return nil
}

func (r *repository) updateWalletBalances(ctx context.Context, tx pgx.Tx, fromID, toID, amount int64) error {
	if _, err := tx.Exec(ctx, `UPDATE wallets SET balance = balance - $1, updated_at = now() WHERE wallet_id = $2`, amount, fromID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE wallets SET balance = balance + $1, updated_at = now() WHERE wallet_id = $2`, amount, toID); err != nil {
		return err
	}
	return nil
}

func (r *repository) markTransactionState(ctx context.Context, tx pgx.Tx, txID int64, state TransactionState) error {
	_, err := tx.Exec(ctx, `UPDATE transactions SET state=$1, updated_at = now() WHERE id=$2`, state, txID)
	return err
}
