package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
	"github.com/lib/pq"
)

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) ExecuteTransferTx(ctx context.Context, transfer *model.Transfer) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Determine lock order to prevent deadlocks
	firstLock, secondLock := transfer.FromWalletID, transfer.ToWalletID
	if firstLock > secondLock {
		firstLock, secondLock = secondLock, firstLock
	}

	// 2. Lock wallets
	var hw1, hw2 model.Wallet
	err = tx.QueryRowContext(ctx, "SELECT id, balance FROM wallets WHERE id = $1 FOR UPDATE", firstLock).Scan(&hw1.ID, &hw1.Balance)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	err = tx.QueryRowContext(ctx, "SELECT id, balance FROM wallets WHERE id = $1 FOR UPDATE", secondLock).Scan(&hw2.ID, &hw2.Balance)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	// Assign source and dest based on original request
	var sourceBalance, destBalance int64
	if hw1.ID == transfer.FromWalletID {
		sourceBalance = hw1.Balance
		destBalance = hw2.Balance
	} else {
		sourceBalance = hw2.Balance
		destBalance = hw1.Balance
	}

	// 3. Balance verification
	if sourceBalance < transfer.Amount {
		// Log failed status but do not proceed with debit/credit. We could save the failed transfer here if required,
		// but typically insufficient funds is just rejected flat out. Let's return error to handler.
		return ErrInsufficientFunds
	}

	// 4. Update balances
	_, err = tx.ExecContext(ctx, "UPDATE wallets SET balance = $1 WHERE id = $2", sourceBalance-transfer.Amount, transfer.FromWalletID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE wallets SET balance = $1 WHERE id = $2", destBalance+transfer.Amount, transfer.ToWalletID)
	if err != nil {
		return err
	}

	// 5. Insert Transfer Record
	transfer.Status = model.StatusProcessed // Successfully processing
	_, err = tx.ExecContext(ctx,
		"INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status) VALUES ($1, $2, $3, $4, $5, $6)",
		transfer.ID, transfer.IdempotencyKey, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount, transfer.Status)
	if err != nil {
		// Check for unique violation (idempotency key clash during concurrent execution)
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return ErrIdempotencyConflict
		}
		return err
	}

	// 6. Insert Ledger entries
	_, err = tx.ExecContext(ctx,
		"INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount) VALUES ($1, $2, $3, $4), ($1, $5, $6, $7)",
		transfer.ID, transfer.FromWalletID, model.EntryTypeDebit, transfer.Amount,
		transfer.ToWalletID, model.EntryTypeCredit, transfer.Amount)
	if err != nil {
		return err
	}

	// 7. Commit
	return tx.Commit()
}

func (r *PostgresRepository) GetTransferByIdempotencyKey(ctx context.Context, key string) (*model.Transfer, error) {
	var t model.Transfer
	err := r.db.QueryRowContext(ctx,
		"SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at FROM transfers WHERE idempotency_key = $1",
		key).Scan(&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (r *PostgresRepository) GetWallet(ctx context.Context, id string) (*model.Wallet, error) {
	var w model.Wallet
	err := r.db.QueryRowContext(ctx, "SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1", id).Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &w, nil
}

func (r *PostgresRepository) CreateWallet(ctx context.Context, wallet *model.Wallet) error {
	_, err := r.db.ExecContext(ctx, "INSERT INTO wallets (id, balance) VALUES ($1, $2)", wallet.ID, wallet.Balance)
	return err
}
