package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"time"

	"wallet-transfer-service/internal/apperror"
	"wallet-transfer-service/internal/model"
)

type TransferRepository interface {
	ExecuteTransfer(ctx context.Context, transfer *model.Transfer, responseBody string) error
	GetStatementByWalletID(ctx context.Context, walletID string) ([]StatementRecord, error)
}

type transferRepository struct {
	db *sql.DB
}

func NewTransferRepository(db *sql.DB) TransferRepository {
	return &transferRepository{db: db}
}

type walletState struct {
	balance int64
	status  model.WalletStatus
}

type StatementRecord struct {
	TransferID    string
	Type          string // "DEBIT" or "CREDIT"
	CounterpartID string
	Amount        int64
	BalanceBefore int64
	BalanceAfter  int64
	Status        model.TransferStatus
	CreatedAt     time.Time // Transaction Date
}

// ExecuteTransfer executes wallet debit/credit, ledger writes, and marks the idempotency
// record as COMPLETED (or FAILED with audit) inside a single atomic SQL transaction.
func (r *transferRepository) ExecuteTransfer(ctx context.Context, transfer *model.Transfer, responseBody string) error {
	slog.DebugContext(ctx, "transfer repository transaction started", "transfer_id", transfer.ID, "from_wallet_id", transfer.FromWalletID, "to_wallet_id", transfer.ToWalletID)
	if transfer.FromWalletID == transfer.ToWalletID {
		slog.WarnContext(ctx, "transfer repository rejected self transfer", "transfer_id", transfer.ID, "wallet_id", transfer.FromWalletID)
		return apperror.ErrSelfTransfer
	}

	// Begin atomic transaction
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		slog.ErrorContext(ctx, "transfer repository failed to begin transaction", "transfer_id", transfer.ID, "error", err)
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback()

	// Lock the idempotency record for the lifetime of this transaction so no
	// concurrent worker can steal/reclaim this lease if execution runs long.
	lockIdempQuery := `
		SELECT status 
		FROM idempotency_records 
		WHERE key = $1 
		FOR UPDATE
	`
	var idempStatus model.IdempotencyStatus
	if err := tx.QueryRowContext(ctx, lockIdempQuery, transfer.IdempotencyKey).Scan(&idempStatus); err != nil {
		slog.ErrorContext(ctx, "transfer repository failed to lock idempotency record", "key", transfer.IdempotencyKey, "error", err)
		return fmt.Errorf("failed to lock idempotency record: %w", err)
	}
	if idempStatus != model.IdempotencyStatusStarted {
		return fmt.Errorf("idempotency record %s has invalid status %s for transfer execution", transfer.IdempotencyKey, idempStatus)
	}

	// Helper to persist rejected business rules into transfers and idempotency_records atomically
	recordFailureAndCommit := func(domainErr error, failureReason string, httpStatus int) error {
		insertFailedTransferQuery := `
			INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`
		if _, err := tx.ExecContext(ctx, insertFailedTransferQuery,
			transfer.ID,
			transfer.IdempotencyKey,
			transfer.FromWalletID,
			transfer.ToWalletID,
			transfer.Amount,
			model.TransferStatusFailed,
			failureReason,
		); err != nil {
			slog.ErrorContext(ctx, "failed to insert failed transfer record", "error", err)
			return fmt.Errorf("failed to insert failed transfer record: %w", err)
		}

		errPayloadBytes, err := json.Marshal(map[string]string{"error": domainErr.Error()})
		if err != nil {
			slog.ErrorContext(ctx, "failed to marshal error payload for idempotency record", "error", err)
			return fmt.Errorf("failed marshaling error payload: %w", err)
		}

		updateIdempQuery := `
			UPDATE idempotency_records 
			SET status = $1, response_code = $2, response_body = $3, updated_at = CURRENT_TIMESTAMP
			WHERE key = $4
		`
		if _, err := tx.ExecContext(ctx, updateIdempQuery,
			model.IdempotencyStatusFailed,
			httpStatus,
			string(errPayloadBytes),
			transfer.IdempotencyKey,
		); err != nil {
			slog.ErrorContext(ctx, "failed to update idempotency record on failure", "error", err)
			return fmt.Errorf("failed updating idempotency record to failed: %w", err)
		}

		if commitErr := tx.Commit(); commitErr != nil {
			slog.ErrorContext(ctx, "failed committing failed transfer audit", "error", commitErr)
			return fmt.Errorf("failed to commit failed transfer audit tx: %w", commitErr)
		}

		transfer.Status = model.TransferStatusFailed
		transfer.FailureReason = &failureReason
		return domainErr
	}

	// Deterministic Lock Ordering:
	// Always sort wallet IDs lexicographically to prevent deadlocks across concurrent transfers.
	firstID, secondID := transfer.FromWalletID, transfer.ToWalletID
	if firstID > secondID {
		firstID, secondID = secondID, firstID
	}

	lockQuery := `
		SELECT id, balance, status 
		FROM wallets 
		WHERE id IN ($1, $2) 
		ORDER BY id 
		FOR UPDATE
	`
	rows, err := tx.QueryContext(ctx, lockQuery, firstID, secondID)
	if err != nil {
		slog.ErrorContext(ctx, "transfer repository failed to acquire wallet locks", "transfer_id", transfer.ID, "error", err)
		return fmt.Errorf("failed to acquire row locks: %w", err)
	}
	defer rows.Close()

	walletsMap := make(map[string]walletState)
	for rows.Next() {
		var id string
		var ws walletState
		if err := rows.Scan(&id, &ws.balance, &ws.status); err != nil {
			return fmt.Errorf("failed scanning wallet row: %w", err)
		}
		walletsMap[id] = ws
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error reading wallet rows: %w", err)
	}

	// Ensure both source and destination accounts exist
	if len(walletsMap) < 2 {
		slog.WarnContext(ctx, "transfer repository rejected transfer: wallet not found", "transfer_id", transfer.ID)
		return recordFailureAndCommit(apperror.ErrWalletNotFound, "wallet not found", http.StatusNotFound)
	}

	fromWallet := walletsMap[transfer.FromWalletID]
	toWallet := walletsMap[transfer.ToWalletID]

	// Source wallet status check
	switch fromWallet.status {
	case model.WalletStatusActive, model.WalletStatusCreditFrozen:
		// Permitted to debit
	case model.WalletStatusFrozen:
		slog.WarnContext(ctx, "transfer repository rejected transfer: source wallet frozen", "transfer_id", transfer.ID)
		return recordFailureAndCommit(apperror.ErrWalletFrozen, "source wallet frozen", http.StatusBadRequest)
	case model.WalletStatusClosed:
		slog.WarnContext(ctx, "transfer repository rejected transfer: source wallet closed", "transfer_id", transfer.ID)
		return recordFailureAndCommit(apperror.ErrWalletClosed, "source wallet closed", http.StatusBadRequest)
	case model.WalletStatusDebitFrozen:
		slog.WarnContext(ctx, "transfer repository rejected transfer: source debit blocked", "transfer_id", transfer.ID)
		return recordFailureAndCommit(apperror.ErrSourceDebitBlocked, "source wallet debit blocked", http.StatusBadRequest)
	default:
		slog.WarnContext(ctx, "transfer repository rejected transfer: source debit blocked", "transfer_id", transfer.ID)
		return recordFailureAndCommit(apperror.ErrSourceDebitBlocked, "source wallet status blocked", http.StatusBadRequest)
	}

	// Destination wallet status check
	switch toWallet.status {
	case model.WalletStatusActive, model.WalletStatusDebitFrozen:
		// Permitted to credit
	case model.WalletStatusFrozen:
		slog.WarnContext(ctx, "transfer repository rejected transfer: destination wallet frozen", "transfer_id", transfer.ID)
		return recordFailureAndCommit(apperror.ErrWalletFrozen, "destination wallet frozen", http.StatusBadRequest)
	case model.WalletStatusClosed:
		slog.WarnContext(ctx, "transfer repository rejected transfer: destination wallet closed", "transfer_id", transfer.ID)
		return recordFailureAndCommit(apperror.ErrWalletClosed, "destination wallet closed", http.StatusBadRequest)
	case model.WalletStatusCreditFrozen:
		slog.WarnContext(ctx, "transfer repository rejected transfer: destination credit blocked", "transfer_id", transfer.ID)
		return recordFailureAndCommit(apperror.ErrDestCreditBlocked, "destination wallet credit blocked", http.StatusBadRequest)
	default:
		slog.WarnContext(ctx, "transfer repository rejected transfer: destination credit blocked", "transfer_id", transfer.ID)
		return recordFailureAndCommit(apperror.ErrDestCreditBlocked, "destination wallet status blocked", http.StatusBadRequest)
	}

	// Balance verification
	if fromWallet.balance < transfer.Amount {
		slog.WarnContext(ctx, "transfer repository rejected transfer: insufficient balance", "transfer_id", transfer.ID)
		return recordFailureAndCommit(apperror.ErrInsufficientBalance, "insufficient balance", http.StatusBadRequest)
	}

	// Guard destination balance against arithmetic overflow past int64 max
	if toWallet.balance > math.MaxInt64-transfer.Amount {
		slog.WarnContext(ctx, "transfer repository rejected transfer: destination balance overflow limit reached", "transfer_id", transfer.ID)
		return recordFailureAndCommit(apperror.ErrBalanceOverflow, "destination balance overflow limit reached", http.StatusBadRequest)
	}

	// Calculate snapshot balances for ledger audit trail
	fromBalanceBefore := fromWallet.balance
	fromBalanceAfter := fromBalanceBefore - transfer.Amount

	toBalanceBefore := toWallet.balance
	toBalanceAfter := toBalanceBefore + transfer.Amount

	// Update wallet balances
	deductQuery := `
		UPDATE wallets 
		SET balance = balance - $1 
		WHERE id = $2
	`
	deductRes, err := tx.ExecContext(ctx, deductQuery, transfer.Amount, transfer.FromWalletID)
	if err != nil {
		return fmt.Errorf("failed to deduct balance: %w", err)
	}
	if rows, err := deductRes.RowsAffected(); err != nil {
		return fmt.Errorf("failed checking affected rows on debit: %w", err)
	} else if rows != 1 {
		return fmt.Errorf("deduct balance affected %d rows; expected 1 for wallet %s", rows, transfer.FromWalletID)
	}

	creditQuery := `
		UPDATE wallets 
		SET balance = balance + $1 
		WHERE id = $2
	`
	creditRes, err := tx.ExecContext(ctx, creditQuery, transfer.Amount, transfer.ToWalletID)
	if err != nil {
		return fmt.Errorf("failed to credit balance: %w", err)
	}
	if rows, err := creditRes.RowsAffected(); err != nil {
		return fmt.Errorf("failed checking affected rows on credit: %w", err)
	} else if rows != 1 {
		return fmt.Errorf("credit balance affected %d rows; expected 1 for wallet %s", rows, transfer.ToWalletID)
	}

	// Record transfer execution
	insertTransferQuery := `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	if _, err := tx.ExecContext(ctx, insertTransferQuery,
		transfer.ID,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		model.TransferStatusProcessed,
	); err != nil {
		return fmt.Errorf("failed to insert transfer record: %w", err)
	}

	// Insert immutable double-entry ledger records
	insertLedgerQuery := `
		INSERT INTO ledger_entries (
			wallet_id, transfer_id, type, amount, balance_before, balance_after, created_at
		)
		VALUES 
			($1, $2, $3, $4, $5, $6, CURRENT_TIMESTAMP),
			($7, $8, $9, $10, $11, $12, CURRENT_TIMESTAMP)
	`
	if _, err := tx.ExecContext(ctx, insertLedgerQuery,
		transfer.FromWalletID, transfer.ID, model.LedgerEntryDebit, transfer.Amount, fromBalanceBefore, fromBalanceAfter,
		transfer.ToWalletID, transfer.ID, model.LedgerEntryCredit, transfer.Amount, toBalanceBefore, toBalanceAfter,
	); err != nil {
		return fmt.Errorf("failed to write ledger entries: %w", err)
	}

	// Atomically finalize idempotency status to COMPLETED inside the same transaction
	updateIdempotencyQuery := `
		UPDATE idempotency_records 
		SET status = $1, response_code = $2, response_body = $3, updated_at = CURRENT_TIMESTAMP
		WHERE key = $4
	`
	res, err := tx.ExecContext(ctx, updateIdempotencyQuery,
		model.IdempotencyStatusCompleted,
		http.StatusCreated,
		responseBody,
		transfer.IdempotencyKey,
	)
	if err != nil {
		return fmt.Errorf("failed to update idempotency record to completed: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to retrieve affected rows for idempotency record: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("idempotency record update affected %d rows; expected exactly 1 for key %s", rowsAffected, transfer.IdempotencyKey)
	}

	// Commit all changes simultaneously
	if err := tx.Commit(); err != nil {
		slog.ErrorContext(ctx, "transfer repository transaction commit failed", "transfer_id", transfer.ID, "error", err)
		return fmt.Errorf("failed to commit transfer transaction: %w", err)
	}

	transfer.Status = model.TransferStatusProcessed
	slog.InfoContext(ctx, "transfer repository transaction committed", "transfer_id", transfer.ID)
	return nil
}

// GetStatementByWalletID reads historical ledger entries using idx_ledger_wallet_created_desc.
func (r *transferRepository) GetStatementByWalletID(ctx context.Context, walletID string) ([]StatementRecord, error) {
	slog.DebugContext(ctx, "statement repository query started", "wallet_id", walletID)
	query := `
		SELECT 
			l.transfer_id,
			l.type,
			CASE 
				WHEN t.from_wallet_id = $1 THEN t.to_wallet_id 
				ELSE t.from_wallet_id 
			END AS counterpart_id,
			l.amount,
			l.balance_before,
			l.balance_after,
			t.status,
			l.created_at
		FROM ledger_entries l
		INNER JOIN transfers t ON l.transfer_id = t.id
		WHERE l.wallet_id = $1
		ORDER BY l.created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, walletID)
	if err != nil {
		slog.ErrorContext(ctx, "statement repository query failed", "wallet_id", walletID, "error", err)
		return nil, fmt.Errorf("failed to query statement: %w", err)
	}
	defer rows.Close()

	var records []StatementRecord
	for rows.Next() {
		var rec StatementRecord
		if err := rows.Scan(
			&rec.TransferID,
			&rec.Type,
			&rec.CounterpartID,
			&rec.Amount,
			&rec.BalanceBefore,
			&rec.BalanceAfter,
			&rec.Status,
			&rec.CreatedAt,
		); err != nil {
			slog.ErrorContext(ctx, "statement repository row scan failed", "wallet_id", walletID, "error", err)
			return nil, fmt.Errorf("failed to scan statement record: %w", err)
		}
		records = append(records, rec)
	}

	return records, rows.Err()
}
