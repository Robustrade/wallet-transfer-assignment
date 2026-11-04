package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"github.com/abhiabhi0/wallet-transfer-assignment/internal/domain"
)

var ErrWalletNotFound = errors.New("wallet not found")

// DBTX is implemented by both *sql.DB and *sql.Tx.
type DBTX interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type WalletStore interface {
	Create(ctx context.Context, wallet domain.Wallet) (domain.Wallet, error)
	GetByID(ctx context.Context, id string) (domain.Wallet, error)
	GetByIDForUpdate(ctx context.Context, id string) (domain.Wallet, error)
	Debit(ctx context.Context, id string, amount domain.Amount) (domain.Wallet, error)
	Credit(ctx context.Context, id string, amount domain.Amount) (domain.Wallet, error)
}

type WalletRepository struct{ db DBTX }

func NewWalletRepository(db DBTX) *WalletRepository { return &WalletRepository{db: db} }

func (r *WalletRepository) Create(ctx context.Context, wallet domain.Wallet) (domain.Wallet, error) {
	if err := wallet.Validate(); err != nil {
		return domain.Wallet{}, fmt.Errorf("validate wallet: %w", err)
	}
	const query = `
		INSERT INTO wallets (id, currency, balance)
		VALUES ($1, $2, $3)
		RETURNING id, currency, balance`
	created, err := scanWallet(r.db.QueryRowContext(ctx, query, wallet.ID, wallet.Currency, int64(wallet.Balance)))
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("create wallet: %w", err)
	}
	return created, nil
}

func (r *WalletRepository) GetByID(ctx context.Context, id string) (domain.Wallet, error) {
	return r.get(ctx, id, false)
}

// GetByIDForUpdate requires a repository backed by *sql.Tx; the row lock is
// held until that transaction commits or rolls back.
func (r *WalletRepository) GetByIDForUpdate(ctx context.Context, id string) (domain.Wallet, error) {
	return r.get(ctx, id, true)
}

func (r *WalletRepository) get(ctx context.Context, id string, forUpdate bool) (domain.Wallet, error) {
	if id == "" {
		return domain.Wallet{}, domain.ErrInvalidWalletID
	}
	query := `SELECT id, currency, balance FROM wallets WHERE id = $1`
	if forUpdate {
		query += " FOR UPDATE"
	}
	wallet, err := scanWallet(r.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Wallet{}, fmt.Errorf("%w: %s", ErrWalletNotFound, id)
	}
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("get wallet %q: %w", id, err)
	}
	return wallet, nil
}

// Debit checks and updates the balance in one statement, preventing overdrafts
// even when multiple requests debit the same wallet concurrently.
func (r *WalletRepository) Debit(ctx context.Context, id string, amount domain.Amount) (domain.Wallet, error) {
	if id == "" {
		return domain.Wallet{}, domain.ErrInvalidWalletID
	}
	if amount <= 0 {
		return domain.Wallet{}, domain.ErrInvalidAmount
	}
	const query = `
		UPDATE wallets
		SET balance = balance - $1, updated_at = NOW()
		WHERE id = $2 AND balance >= $1
		RETURNING id, currency, balance`
	wallet, err := scanWallet(r.db.QueryRowContext(ctx, query, int64(amount), id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Wallet{}, r.classifyMissingUpdate(ctx, id, domain.ErrInsufficientFunds)
	}
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("debit wallet %q: %w", id, err)
	}
	return wallet, nil
}

// Credit checks for BIGINT overflow and updates the balance atomically.
func (r *WalletRepository) Credit(ctx context.Context, id string, amount domain.Amount) (domain.Wallet, error) {
	if id == "" {
		return domain.Wallet{}, domain.ErrInvalidWalletID
	}
	if amount <= 0 {
		return domain.Wallet{}, domain.ErrInvalidAmount
	}
	const query = `
		UPDATE wallets
		SET balance = balance + $1, updated_at = NOW()
		WHERE id = $2 AND balance <= $3::BIGINT - $1::BIGINT
		RETURNING id, currency, balance`
	wallet, err := scanWallet(r.db.QueryRowContext(ctx, query, int64(amount), id, int64(math.MaxInt64)))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Wallet{}, r.classifyMissingUpdate(ctx, id, domain.ErrBalanceOverflow)
	}
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("credit wallet %q: %w", id, err)
	}
	return wallet, nil
}

func (r *WalletRepository) classifyMissingUpdate(ctx context.Context, id string, rejected error) error {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM wallets WHERE id = $1)`, id).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check wallet %q after rejected balance update: %w", id, err)
	}
	if !exists {
		return fmt.Errorf("%w: %s", ErrWalletNotFound, id)
	}
	return rejected
}

func scanWallet(row *sql.Row) (domain.Wallet, error) {
	var wallet domain.Wallet
	var balance int64
	if err := row.Scan(&wallet.ID, &wallet.Currency, &balance); err != nil {
		return domain.Wallet{}, err
	}
	wallet.Balance = domain.Amount(balance)
	return wallet, nil
}
