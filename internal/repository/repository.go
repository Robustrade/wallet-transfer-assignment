package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/wallet-transfer-assignment/internal/model"
)

var (
	ErrInsufficientFunds    = errors.New("insufficient funds")
	ErrWalletNotFound       = errors.New("wallet not found")
	ErrIdempotencyKeyLocked = errors.New("idempotency key is currently locked by another request")
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// LockIdempotencyKey attempts to insert the idempotency key or retrieve the existing one
func (r *Repository) LockIdempotencyKey(ctx context.Context, key string) (*model.IdempotencyKey, error) {
	query := `
		INSERT INTO idempotency_keys (idempotency_key)
		VALUES ($1)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING idempotency_key, locked_at, response_code, response_body
	`
	var idk model.IdempotencyKey
	var respCode sql.NullInt32
	var respBody []byte

	err := r.db.QueryRowContext(ctx, query, key).Scan(&idk.IdempotencyKey, &idk.LockedAt, &respCode, &respBody)
	if err == sql.ErrNoRows {
		// Conflict occurred and DO NOTHING was executed, fetch the existing key
		queryGet := `SELECT idempotency_key, locked_at, response_code, response_body FROM idempotency_keys WHERE idempotency_key = $1`
		err = r.db.QueryRowContext(ctx, queryGet, key).Scan(&idk.IdempotencyKey, &idk.LockedAt, &respCode, &respBody)
		if err != nil {
			return nil, err
		}

		if !respCode.Valid {
			// The key exists but has no response yet. Another request is currently processing it.
			return nil, ErrIdempotencyKeyLocked
		}
	} else if err != nil {
		return nil, err
	}

	if respCode.Valid {
		v := int(respCode.Int32)
		idk.ResponseCode = &v
		idk.ResponseBody = respBody
	}
	return &idk, nil
}

// UpdateIdempotencyKey updates the key with the final response
func (r *Repository) UpdateIdempotencyKey(ctx context.Context, key string, code int, body json.RawMessage) error {
	query := `UPDATE idempotency_keys SET response_code = $1, response_body = $2 WHERE idempotency_key = $3`
	_, err := r.db.ExecContext(ctx, query, code, body, key)
	return err
}

// ExecuteTransfer handles the safe transactional transfer
func (r *Repository) ExecuteTransfer(ctx context.Context, transfer *model.Transfer) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Lock wallets in consistent order to prevent deadlocks
	first, second := transfer.FromWalletID, transfer.ToWalletID
	if first > second {
		first, second = second, first
	}

	queryLock := `SELECT id, balance FROM wallets WHERE id = $1 FOR UPDATE`

	// Lock first
	var w1 model.Wallet
	err = tx.QueryRowContext(ctx, queryLock, first).Scan(&w1.ID, &w1.Balance)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrWalletNotFound
		}
		return err
	}

	// Lock second
	var w2 model.Wallet
	err = tx.QueryRowContext(ctx, queryLock, second).Scan(&w2.ID, &w2.Balance)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrWalletNotFound
		}
		return err
	}

	// 2. Insert transfer as PENDING
	queryInsertTransfer := `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status)
		VALUES ($1, $2, $3, $4, $5, 'PENDING')
	`
	_, err = tx.ExecContext(ctx, queryInsertTransfer, transfer.ID, transfer.IdempotencyKey, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount)
	if err != nil {
		return err
	}

	var fromBalance float64
	if w1.ID == transfer.FromWalletID {
		fromBalance = w1.Balance
	} else {
		fromBalance = w2.Balance
	}

	// 3. Check sufficient funds
	if fromBalance < transfer.Amount {
		// Update transfer to FAILED
		_, _ = tx.ExecContext(ctx, `UPDATE transfers SET status = 'FAILED' WHERE id = $1`, transfer.ID)
		tx.Commit() // Commit the failure state
		return ErrInsufficientFunds
	}

	// 4. Update balances
	queryUpdateWallet := `UPDATE wallets SET balance = balance + $1 WHERE id = $2`
	_, err = tx.ExecContext(ctx, queryUpdateWallet, -transfer.Amount, transfer.FromWalletID)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, queryUpdateWallet, transfer.Amount, transfer.ToWalletID)
	if err != nil {
		return err
	}

	// 5. Insert Ledger Entries
	queryInsertLedger := `INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount) VALUES ($1, $2, $3, $4)`
	_, err = tx.ExecContext(ctx, queryInsertLedger, transfer.ID, transfer.FromWalletID, "DEBIT", transfer.Amount)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, queryInsertLedger, transfer.ID, transfer.ToWalletID, "CREDIT", transfer.Amount)
	if err != nil {
		return err
	}

	// 6. Update transfer to PROCESSED
	_, err = tx.ExecContext(ctx, `UPDATE transfers SET status = 'PROCESSED' WHERE id = $1`, transfer.ID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *Repository) GetWallet(ctx context.Context, id string) (*model.Wallet, error) {
	var w model.Wallet
	err := r.db.QueryRowContext(ctx, "SELECT id, balance, created_at FROM wallets WHERE id = $1", id).Scan(&w.ID, &w.Balance, &w.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *Repository) CreateWallet(ctx context.Context, id string, balance float64) error {
	_, err := r.db.ExecContext(ctx, "INSERT INTO wallets (id, balance) VALUES ($1, $2)", id, balance)
	return err
}
