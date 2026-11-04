package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"wallet-transfer/internal/domain"
)

type TransferRepository struct{ db *sql.DB }

func NewTransferRepository(db *sql.DB) *TransferRepository { return &TransferRepository{db: db} }

func (r *TransferRepository) CreateWallet(ctx context.Context, wallet *domain.Wallet) error {
	if wallet == nil || wallet.ID == "" || wallet.Balance < 0 {
		return domain.ErrInvalidTransfer
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO wallets (id, balance) VALUES (?, ?)`, wallet.ID, wallet.Balance)
	if err != nil {
		return fmt.Errorf("create wallet: %w", err)
	}
	return nil
}

func (r *TransferRepository) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	wallet := &domain.Wallet{}
	err := r.db.QueryRowContext(ctx, `SELECT id, balance FROM wallets WHERE id = ?`, id).Scan(&wallet.ID, &wallet.Balance)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}
	return wallet, nil
}

// TransferFunds serializes the idempotency decision and all financial side
// effects in one SQLite transaction. The conditional UPDATE is the final guard
// against overdrafts; no Go mutex is needed for database correctness.
func (r *TransferRepository) TransferFunds(ctx context.Context, request *domain.Transfer) (*domain.Transfer, error) {
	if request == nil || request.IdempotencyKey == "" || request.FromWalletID == "" || request.ToWalletID == "" || request.FromWalletID == request.ToWalletID || request.Amount <= 0 {
		return nil, domain.ErrInvalidTransfer
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transfer transaction: %w", err)
	}
	defer tx.Rollback()

	var fromID, toID string
	var amount int64
	var status domain.IdempotencyStatus
	var failureReason sql.NullString
	var transferID sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT from_wallet_id, to_wallet_id, amount, status, failure_reason, transfer_id
		FROM idempotency_records WHERE idempotency_key = ?`, request.IdempotencyKey).
		Scan(&fromID, &toID, &amount, &status, &failureReason, &transferID)
	if err == nil {
		if fromID != request.FromWalletID || toID != request.ToWalletID || amount != request.Amount {
			return nil, domain.ErrIdempotencyConflict
		}
		if status == domain.IdempotencyStatusCompleted {
			completed, getErr := getTransferByID(ctx, tx, transferID.String)
			if getErr != nil {
				return nil, getErr
			}
			return completed, nil
		}
		// PROCESSING is not normally observable because it is written and
		// finalized in the same transaction. A committed retryable failure
		// deliberately falls through and re-evaluates the current balance.
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("lookup idempotency record: %w", err)
	} else {
		if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records
			(idempotency_key, from_wallet_id, to_wallet_id, amount, status)
			VALUES (?, ?, ?, ?, ?)`, request.IdempotencyKey, request.FromWalletID,
			request.ToWalletID, request.Amount, domain.IdempotencyStatusProcessing); err != nil {
			return nil, fmt.Errorf("claim idempotency key: %w", err)
		}
	}

	// Validate both wallets before mutating either balance.
	var sourceExists, destinationExists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM wallets WHERE id = ?`, request.FromWalletID).Scan(&sourceExists); err != nil {
		return nil, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM wallets WHERE id = ?`, request.ToWalletID).Scan(&destinationExists); err != nil {
		return nil, err
	}
	if sourceExists == 0 || destinationExists == 0 {
		return nil, domain.ErrWalletNotFound
	}

	result, err := tx.ExecContext(ctx, `UPDATE wallets SET balance = balance - ? WHERE id = ? AND balance >= ?`, request.Amount, request.FromWalletID, request.Amount)
	if err != nil {
		return nil, fmt.Errorf("conditionally debit source wallet: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("read debit result: %w", err)
	}
	if rows == 0 {
		_, err = tx.ExecContext(ctx, `UPDATE idempotency_records SET status = ?, failure_reason = ?, transfer_id = NULL, updated_at = CURRENT_TIMESTAMP WHERE idempotency_key = ?`,
			domain.IdempotencyStatusRetryableFailed, domain.IdempotencyFailureInsufficientFunds, request.IdempotencyKey)
		if err != nil {
			return nil, fmt.Errorf("save retryable insufficient-funds result: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit retryable failure: %w", err)
		}
		failed := *request
		failed.Status = domain.TransferStatusFailed
		return &failed, domain.ErrInsufficientFunds
	}

	result, err = tx.ExecContext(ctx, `UPDATE wallets SET balance = balance + ? WHERE id = ?`, request.Amount, request.ToWalletID)
	if err != nil {
		return nil, fmt.Errorf("credit destination wallet: %w", err)
	}
	rows, err = result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("read credit result: %w", err)
	}
	if rows != 1 {
		return nil, domain.ErrWalletNotFound
	}

	id, err := newID()
	if err != nil {
		return nil, fmt.Errorf("generate transfer id: %w", err)
	}
	transfer := &domain.Transfer{ID: id, IdempotencyKey: request.IdempotencyKey, FromWalletID: request.FromWalletID, ToWalletID: request.ToWalletID, Amount: request.Amount, Status: domain.TransferStatusProcessed}
	if _, err := tx.ExecContext(ctx, `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status) VALUES (?, ?, ?, ?, ?)`, transfer.ID, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount, transfer.Status); err != nil {
		return nil, fmt.Errorf("insert transfer: %w", err)
	}
	for _, entry := range []struct {
		walletID string
		kind     domain.LedgerEntryType
	}{{transfer.FromWalletID, domain.LedgerEntryDebit}, {transfer.ToWalletID, domain.LedgerEntryCredit}} {
		entryID, idErr := newID()
		if idErr != nil {
			return nil, fmt.Errorf("generate ledger id: %w", idErr)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ledger_entries (id, wallet_id, transfer_id, type, amount) VALUES (?, ?, ?, ?, ?)`, entryID, entry.walletID, transfer.ID, entry.kind, transfer.Amount); err != nil {
			return nil, fmt.Errorf("insert %s ledger entry: %w", entry.kind, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE idempotency_records SET status = ?, failure_reason = NULL, transfer_id = ?, updated_at = CURRENT_TIMESTAMP WHERE idempotency_key = ?`,
		domain.IdempotencyStatusCompleted, transfer.ID, request.IdempotencyKey); err != nil {
		return nil, fmt.Errorf("complete idempotency record: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transfer: %w", err)
	}
	return transfer, nil
}

func (r *TransferRepository) GetTransferByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	return getTransferByKey(ctx, r.db, key)
}

func (r *TransferRepository) ListLedgerEntries(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, wallet_id, transfer_id, type, amount FROM ledger_entries WHERE wallet_id = ? ORDER BY created_at, id`, walletID)
	if err != nil {
		return nil, fmt.Errorf("list ledger entries: %w", err)
	}
	defer rows.Close()
	entries := make([]domain.LedgerEntry, 0)
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.WalletID, &e.TransferID, &e.Type, &e.Amount); err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ledger entries: %w", err)
	}
	return entries, nil
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getTransferByKey(ctx context.Context, q queryRower, key string) (*domain.Transfer, error) {
	var transferID string
	err := q.QueryRowContext(ctx, `SELECT transfer_id FROM idempotency_records WHERE idempotency_key = ? AND status = ?`, key, domain.IdempotencyStatusCompleted).Scan(&transferID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrTransferNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get completed transfer by idempotency key: %w", err)
	}
	return getTransferByID(ctx, q, transferID)
}

func getTransferByID(ctx context.Context, q queryRower, id string) (*domain.Transfer, error) {
	transfer := &domain.Transfer{}
	err := q.QueryRowContext(ctx, `SELECT id, from_wallet_id, to_wallet_id, amount, status FROM transfers WHERE id = ?`, id).
		Scan(&transfer.ID, &transfer.FromWalletID, &transfer.ToWalletID, &transfer.Amount, &transfer.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrTransferNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get transfer by id: %w", err)
	}
	return transfer, nil
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
