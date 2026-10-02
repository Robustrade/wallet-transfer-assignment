package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"wallet-service/internal/domain"
	"wallet-service/internal/repository"

	"github.com/google/uuid"
)

type SQLiteRepository struct {
	db *sql.DB
}

func NewSQLiteRepository(db *sql.DB) repository.Repository {
	return &SQLiteRepository{db: db}
}

func (r *SQLiteRepository) ExecuteTransfer(ctx context.Context, transferReq *domain.Transfer) (*domain.Transfer, error) {
	// SQLite supports database level locking, transactions are EXCLUSIVE by default
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// 1. Check source wallet
	var fromWallet domain.Wallet
	err = tx.QueryRowContext(ctx, "SELECT id, balance FROM wallets WHERE id = ?", transferReq.FromWalletID).Scan(&fromWallet.ID, &fromWallet.Balance)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, err
	}

	// 2. Check destination wallet
	var toWallet domain.Wallet
	err = tx.QueryRowContext(ctx, "SELECT id, balance FROM wallets WHERE id = ?", transferReq.ToWalletID).Scan(&toWallet.ID, &toWallet.Balance)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, err
	}

	if fromWallet.Balance < transferReq.Amount {
		return nil, domain.ErrInsufficientFunds
	}

	// 3. Update balances
	_, err = tx.ExecContext(ctx, "UPDATE wallets SET balance = balance - ?, updated_at = ? WHERE id = ?", transferReq.Amount, time.Now(), transferReq.FromWalletID)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE wallets SET balance = balance + ?, updated_at = ? WHERE id = ?", transferReq.Amount, time.Now(), transferReq.ToWalletID)
	if err != nil {
		return nil, err
	}

	// 4. Create Transfer record
	transferReq.ID = uuid.NewString()
	transferReq.State = domain.TransferStateProcessed
	now := time.Now()
	transferReq.CreatedAt = now
	transferReq.UpdatedAt = now

	_, err = tx.ExecContext(ctx, `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, created_at, updated_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, transferReq.ID, transferReq.IdempotencyKey, transferReq.FromWalletID, transferReq.ToWalletID, transferReq.Amount, transferReq.State, transferReq.CreatedAt, transferReq.UpdatedAt)
	if err != nil {
		if err.Error() == "UNIQUE constraint failed: transfers.idempotency_key" {
			return nil, domain.ErrDuplicateTransfer
		}
		return nil, err
	}

	// 5. Create Ledger entries
	_, err = tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (id, wallet_id, transfer_id, type, amount, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, uuid.NewString(), transferReq.FromWalletID, transferReq.ID, domain.EntryTypeDebit, transferReq.Amount, now)
	if err != nil {
		return nil, err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (id, wallet_id, transfer_id, type, amount, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, uuid.NewString(), transferReq.ToWalletID, transferReq.ID, domain.EntryTypeCredit, transferReq.Amount, now)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return transferReq, nil
}

func (r *SQLiteRepository) GetTransferByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	var transfer domain.Transfer
	err := r.db.QueryRowContext(ctx, "SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, created_at, updated_at FROM transfers WHERE idempotency_key = ?", key).
		Scan(&transfer.ID, &transfer.IdempotencyKey, &transfer.FromWalletID, &transfer.ToWalletID, &transfer.Amount, &transfer.State, &transfer.CreatedAt, &transfer.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &transfer, nil
}
