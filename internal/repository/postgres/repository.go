package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"wallet-service/internal/domain"
	"wallet-service/internal/repository"

	"github.com/google/uuid"
)

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) repository.Repository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) ExecuteTransfer(ctx context.Context, transferReq *domain.Transfer) (*domain.Transfer, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Lock wallets in consistent order to prevent deadlocks
	firstWalletID := transferReq.FromWalletID
	secondWalletID := transferReq.ToWalletID
	if firstWalletID > secondWalletID {
		firstWalletID, secondWalletID = secondWalletID, firstWalletID
	}

	var firstBalance, secondBalance int64
	err = tx.QueryRowContext(ctx, "SELECT balance FROM wallets WHERE id = $1 FOR UPDATE", firstWalletID).Scan(&firstBalance)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, err
	}

	err = tx.QueryRowContext(ctx, "SELECT balance FROM wallets WHERE id = $1 FOR UPDATE", secondWalletID).Scan(&secondBalance)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, err
	}

	var fromWallet domain.Wallet

	if firstWalletID == transferReq.FromWalletID {
		fromWallet = domain.Wallet{ID: firstWalletID, Balance: firstBalance}
	} else {
		fromWallet = domain.Wallet{ID: secondWalletID, Balance: secondBalance}
	}

	if fromWallet.Balance < transferReq.Amount {
		return nil, domain.ErrInsufficientFunds
	}

	// 3. Update balances
	_, err = tx.ExecContext(ctx, "UPDATE wallets SET balance = balance - $1, updated_at = $2 WHERE id = $3", transferReq.Amount, time.Now(), transferReq.FromWalletID)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE wallets SET balance = balance + $1, updated_at = $2 WHERE id = $3", transferReq.Amount, time.Now(), transferReq.ToWalletID)
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
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, transferReq.ID, transferReq.IdempotencyKey, transferReq.FromWalletID, transferReq.ToWalletID, transferReq.Amount, transferReq.State, transferReq.CreatedAt, transferReq.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "transfers_idempotency_key_key") || strings.Contains(err.Error(), "UNIQUE constraint failed: transfers.idempotency_key") {
			return nil, domain.ErrDuplicateTransfer
		}
		return nil, err
	}

	// 5. Create Ledger entries
	_, err = tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (id, wallet_id, transfer_id, type, amount, created_at)
		VALUES ($1, $2, $3, $4, $5, $6), ($7, $8, $9, $10, $11, $12)
	`, uuid.NewString(), transferReq.FromWalletID, transferReq.ID, domain.EntryTypeDebit, transferReq.Amount, now,
		uuid.NewString(), transferReq.ToWalletID, transferReq.ID, domain.EntryTypeCredit, transferReq.Amount, now)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return transferReq, nil
}

func (r *PostgresRepository) GetTransferByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	var transfer domain.Transfer
	err := r.db.QueryRowContext(ctx, "SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, created_at, updated_at FROM transfers WHERE idempotency_key = $1", key).
		Scan(&transfer.ID, &transfer.IdempotencyKey, &transfer.FromWalletID, &transfer.ToWalletID, &transfer.Amount, &transfer.State, &transfer.CreatedAt, &transfer.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &transfer, nil
}
