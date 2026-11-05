package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"wallet-transfer/internal/domain"
)

type TransferRepository struct{ db *sql.DB }

func NewTransferRepository(db *sql.DB) *TransferRepository { return &TransferRepository{db: db} }

func (r *TransferRepository) CreateWallet(ctx context.Context, wallet *domain.Wallet) error {
	if wallet == nil || strings.TrimSpace(wallet.ID) == "" || wallet.Balance < 0 {
		return domain.ErrInvalidWallet
	}
	if wallet.Status == "" {
		wallet.Status = domain.WalletStatusActive
	}
	if wallet.Status != domain.WalletStatusActive && wallet.Status != domain.WalletStatusInactive {
		return domain.ErrInvalidWallet
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin wallet creation: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `INSERT INTO wallets (id, balance, status) VALUES (?, ?, ?) ON CONFLICT(id) DO NOTHING`,
		wallet.ID, wallet.Balance, wallet.Status)
	if err != nil {
		return fmt.Errorf("create wallet: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check wallet creation: %w", err)
	}
	if inserted == 0 {
		return domain.ErrWalletAlreadyExists
	}
	if wallet.Balance > 0 {
		entryID, err := newID()
		if err != nil {
			return fmt.Errorf("generate opening balance ledger id: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ledger_entries (id, wallet_id, transfer_id, type, amount) VALUES (?, ?, NULL, ?, ?)`,
			entryID, wallet.ID, domain.LedgerEntryCredit, wallet.Balance); err != nil {
			return fmt.Errorf("record opening balance: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit wallet creation: %w", err)
	}
	return nil
}

func (r *TransferRepository) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	wallet := &domain.Wallet{}
	err := r.db.QueryRowContext(ctx, `SELECT id, balance, status, created_at, updated_at FROM wallets WHERE id = ?`, id).
		Scan(&wallet.ID, &wallet.Balance, &wallet.Status, &wallet.CreatedAt, &wallet.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}
	return wallet, nil
}

func (r *TransferRepository) AddFunds(ctx context.Context, walletID string, amount int64) (*domain.Wallet, error) {
	if strings.TrimSpace(walletID) == "" || amount <= 0 {
		return nil, domain.ErrInvalidWallet
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin wallet funding: %w", err)
	}
	defer tx.Rollback()

	var status domain.WalletStatus
	err = tx.QueryRowContext(ctx, `SELECT status FROM wallets WHERE id = ?`, walletID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get wallet before funding: %w", err)
	}
	if status != domain.WalletStatusActive {
		return nil, domain.ErrWalletInactive
	}
	result, err := tx.ExecContext(ctx, `UPDATE wallets SET balance = balance + ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = 'ACTIVE'`,
		amount, walletID)
	if err != nil {
		return nil, fmt.Errorf("add wallet funds: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("check wallet funding update: %w", err)
	}
	if updated != 1 {
		return nil, domain.ErrWalletInactive
	}
	entryID, err := newID()
	if err != nil {
		return nil, fmt.Errorf("generate funding ledger id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO ledger_entries (id, wallet_id, transfer_id, type, amount) VALUES (?, ?, NULL, ?, ?)`,
		entryID, walletID, domain.LedgerEntryCredit, amount); err != nil {
		return nil, fmt.Errorf("record wallet funding: %w", err)
	}
	wallet := &domain.Wallet{}
	if err := tx.QueryRowContext(ctx, `SELECT id, balance, status, created_at, updated_at FROM wallets WHERE id = ?`, walletID).
		Scan(&wallet.ID, &wallet.Balance, &wallet.Status, &wallet.CreatedAt, &wallet.UpdatedAt); err != nil {
		return nil, fmt.Errorf("read funded wallet: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit wallet funding: %w", err)
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

	// Both wallets must exist and be active before either balance changes.
	var sourceStatus, destinationStatus domain.WalletStatus
	if err := tx.QueryRowContext(ctx, `SELECT status FROM wallets WHERE id = ?`, request.FromWalletID).Scan(&sourceStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("read source wallet: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT status FROM wallets WHERE id = ?`, request.ToWalletID).Scan(&destinationStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("read destination wallet: %w", err)
	}
	if sourceStatus != domain.WalletStatusActive || destinationStatus != domain.WalletStatusActive {
		return nil, domain.ErrWalletInactive
	}

	result, err := tx.ExecContext(ctx, `UPDATE wallets SET balance = balance - ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND balance >= ? AND status = 'ACTIVE'`, request.Amount, request.FromWalletID, request.Amount)
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

	result, err = tx.ExecContext(ctx, `UPDATE wallets SET balance = balance + ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = 'ACTIVE'`, request.Amount, request.ToWalletID)
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
		var transferID sql.NullString
		if err := rows.Scan(&e.ID, &e.WalletID, &transferID, &e.Type, &e.Amount); err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", err)
		}
		if transferID.Valid {
			e.TransferID = transferID.String
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
