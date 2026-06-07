package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Saket121/wallet-transfer-assignment/internal/domain"
)

type SQLiteRepository struct {
	db *sql.DB
	mu sync.Mutex
}

func NewSQLiteRepository(db *sql.DB) *SQLiteRepository {
	return &SQLiteRepository{db: db}
}

// FindTransferByIdempotencyKey looks up an existing transfer by its idempotency key.
func (r *SQLiteRepository) FindTransferByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	query := `
        SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
        FROM transfers
        WHERE idempotency_key = ?
    `

	var t domain.Transfer
	var failureReason sql.NullString
	var createdAt, updatedAt string

	err := r.db.QueryRowContext(ctx, query, key).Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID,
		&t.Amount, &t.Status, &failureReason, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding transfer by idempotency key: %w", err)
	}

	if failureReason.Valid {
		t.FailureReason = failureReason.String
	}
	t.CreatedAt, _ = time.Parse(time.DateTime, createdAt)
	t.UpdatedAt, _ = time.Parse(time.DateTime, updatedAt)
	return &t, nil
}

func (r *SQLiteRepository) ExecuteTransfer(ctx context.Context, transfer *domain.Transfer) (*domain.Transfer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	walletIDs := []string{transfer.FromWalletID, transfer.ToWalletID}
	sort.Strings(walletIDs)

	wallets := make(map[string]int64, 2)
	for _, wid := range walletIDs {
		var balance int64
		err = tx.QueryRowContext(ctx, `SELECT balance FROM wallets WHERE id = ?`, wid).Scan(&balance)
		if err != nil {
			return nil, fmt.Errorf("reading wallet %s: %w", wid, err)
		}
		wallets[wid] = balance
	}

	now := time.Now().UTC().Format(time.DateTime)
	transferID := uuid.New().String()

	_, err = tx.ExecContext(ctx,
		`INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		transferID, transfer.IdempotencyKey, transfer.FromWalletID, transfer.ToWalletID,
		transfer.Amount, domain.TransferStatusPending, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting transfer: %w", err)
	}
	transfer.ID = transferID
	transfer.Status = domain.TransferStatusPending
	transfer.CreatedAt, _ = time.Parse(time.DateTime, now)
	transfer.UpdatedAt = transfer.CreatedAt

	senderBalance := wallets[transfer.FromWalletID]
	if senderBalance < transfer.Amount {
		failReason := "insufficient funds"
		_, err = tx.ExecContext(ctx,
			`UPDATE transfers SET status = ?, failure_reason = ?, updated_at = ? WHERE id = ?`,
			domain.TransferStatusFailed, failReason, now, transferID,
		)
		if err != nil {
			return nil, fmt.Errorf("marking transfer as failed: %w", err)
		}
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("committing failed transfer: %w", err)
		}
		transfer.Status = domain.TransferStatusFailed
		transfer.FailureReason = failReason
		return transfer, nil
	}

	// Update balances
	_, err = tx.ExecContext(ctx,
		`UPDATE wallets SET balance = balance - ?, updated_at = ? WHERE id = ?`,
		transfer.Amount, now, transfer.FromWalletID,
	)
	if err != nil {
		return nil, fmt.Errorf("debiting wallet: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`UPDATE wallets SET balance = balance + ?, updated_at = ? WHERE id = ?`,
		transfer.Amount, now, transfer.ToWalletID,
	)
	if err != nil {
		return nil, fmt.Errorf("crediting wallet: %w", err)
	}

	// Insert ledger entries
	_, err = tx.ExecContext(ctx,
		`INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount, created_at)
         VALUES (?, ?, ?, ?, ?, ?)`,
		uuid.New().String(), transferID, transfer.FromWalletID, domain.EntryTypeDebit, transfer.Amount, now,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting debit ledger entry: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount, created_at)
         VALUES (?, ?, ?, ?, ?, ?)`,
		uuid.New().String(), transferID, transfer.ToWalletID, domain.EntryTypeCredit, transfer.Amount, now,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting credit ledger entry: %w", err)
	}

	// Mark transfer as PROCESSED
	_, err = tx.ExecContext(ctx,
		`UPDATE transfers SET status = ?, updated_at = ? WHERE id = ?`,
		domain.TransferStatusProcessed, now, transferID,
	)
	if err != nil {
		return nil, fmt.Errorf("marking transfer as processed: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing transfer: %w", err)
	}

	transfer.Status = domain.TransferStatusProcessed
	return transfer, nil
}

// GetWallet retrieves a wallet by ID.
func (r *SQLiteRepository) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	query := `SELECT id, balance, currency, created_at, updated_at FROM wallets WHERE id = ?`

	var w domain.Wallet
	var createdAt, updatedAt string

	err := r.db.QueryRowContext(ctx, query, id).Scan(&w.ID, &w.Balance, &w.Currency, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting wallet: %w", err)
	}
	w.CreatedAt, _ = time.Parse(time.DateTime, createdAt)
	w.UpdatedAt, _ = time.Parse(time.DateTime, updatedAt)
	return &w, nil
}
