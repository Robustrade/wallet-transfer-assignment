package datasource

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	db "wallet-transfer-assigment/internal/db/generated"
	"wallet-transfer-assigment/utils"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	maxSQLiteLockRetries = 5
	initialSQLiteRetry   = 10 * time.Millisecond
)

type transactioner interface {
	GetTransactions(ctx context.Context) ([]db.Transfer, error)
	Transfer(ctx context.Context, request db.CreateTransferParams, idempotencyKey string) (*string, error)
}

func (ds DataSource) GetTransactions(ctx context.Context) ([]db.Transfer, error) {
	statements, err := ds.Queries.GetTransactions(ctx)
	if err != nil {
		ds.log.Error(" unable to fetch statement record", zap.Error(err))
		return nil, err
	}
	return statements, nil
}

func (ds DataSource) Transfer(ctx context.Context, req db.CreateTransferParams, idempotencyKey string) (*string, error) {
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {

		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	ds.log.Debug("Transaction beign", zap.Any("request", req))
	defer func() {
		_ = tx.Rollback()
	}()

	q := ds.Queries.WithTx(tx)
	// idempotency check
	idempotencyRecord, err := q.GetIdempotencyRecordById(ctx, idempotencyKey)
	if err == nil {
		ds.log.Info("found idempotency record")
		return &idempotencyRecord.TransactionID, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		ds.log.Warn(" failed to check idempotency key")
		return nil, fmt.Errorf("failed to check idempotency key: %w",
			err,
		)
	}
	ds.log.Debug("no record found in idempotency")

	// create new transaction ID
	transactionID := uuid.New().String()
	ds.log.Info("transaction id generated successfully", zap.String("transactionId", transactionID))
	req.TransactionID = transactionID
	_, err = q.CreateTransfer(ctx, req) // pending status
	if err != nil {
		return nil, fmt.Errorf("create transfer: %w", err)
	}
	// create Idempotency
	err = q.CreateIdempotencyRecord(ctx, db.CreateIdempotencyRecordParams{
		IdempotencyKey: idempotencyKey,
		TransactionID:  transactionID,
	})
	if err != nil {
		ds.log.Error("failed to create  Idempotency Key", zap.String("IdempotencyKey", idempotencyKey))
		return nil, fmt.Errorf("failed to create  Idempotency Key:%w ", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	ds.log.Info(
		"transaction committed successfully",
		zap.String("transaction_id", transactionID),
	)
	// process transfer
	err = processTransfer(ctx, ds, req, transactionID)
	if err != nil {
		ds.log.Error(
			"transfer processing failed",
			zap.String("transaction_id", transactionID),
			zap.Error(err),
		)

		if failedErr := markTransferFailed(ctx, ds, transactionID, err.Error()); failedErr != nil {
			ds.log.Error(
				"failed to mark transfer as FAILED",
				zap.String("transaction_id", transactionID),
				zap.Error(failedErr),
			)

			return nil, fmt.Errorf(
				"transfer failed: %w; additionally failed to mark FAILED: %v",
				err,
				failedErr,
			)
		}

		return nil, err
	}

	return &transactionID, nil
}

func markTransferFailed(ctx context.Context, ds DataSource, transactionID, comment string) error {
	return retrySQLiteLock(ctx, func() error {
		return markTransferFailedOnce(ctx, ds, transactionID, comment)
	})
}

func markTransferFailedOnce(ctx context.Context, ds DataSource, transactionID, comment string) error {
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin failed-status transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	q := ds.Queries.WithTx(tx)

	rows, err := q.MarkTransferFailed(ctx, db.MarkTransferFailedParams{
		Comments: sql.NullString{
			String: comment,
			Valid:  comment != "",
		},
		TransactionID: transactionID,
	})
	if err != nil {
		return fmt.Errorf("mark transfer failed: %w", err)
	}

	if rows != 1 {
		return fmt.Errorf("failed to transition transaction %s to FAILED", transactionID)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit failed status: %w", err)
	}

	return nil
}

func processTransfer(ctx context.Context, ds DataSource, req db.CreateTransferParams, transactionID string) error {
	return retrySQLiteLock(ctx, func() error {
		return processTransferOnce(ctx, ds, req, transactionID)
	})
}

func processTransferOnce(ctx context.Context, ds DataSource, req db.CreateTransferParams, transactionID string) error {
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	q := ds.Queries.WithTx(tx)

	// Fetch sender
	senderWallet, err := q.GetWallet(ctx, req.FromWalletID)
	if err != nil {
		return fmt.Errorf("failed to fetch sender wallet: %w", err)
	}

	// Fetch receiver
	receiverWallet, err := q.GetWallet(ctx, req.ToWalletID)
	if err != nil {
		return fmt.Errorf("failed to fetch receiver wallet: %w", err)
	}

	// Atomic debit
	senderBalance, err := q.DebitWallet(ctx, db.DebitWalletParams{
		Balance:   req.Amount,
		WalletID:  senderWallet.WalletID,
		Balance_2: req.Amount,
	})
	if err != nil {
		return fmt.Errorf("failed to debit wallet: %w", err)
	}
	if senderBalance != 1 {
		return fmt.Errorf("insufficient balance")
	}

	// Atomic credit
	receiverBalance, err := q.CreditWallet(ctx, db.CreditWalletParams{
		Balance:  req.Amount,
		WalletID: receiverWallet.WalletID,
	})
	if err != nil {
		return fmt.Errorf("failed to credit wallet: %w", err)
	}
	if receiverBalance != 1 {
		return fmt.Errorf("failed to credit wallet")
	}

	// Debit ledger
	err = q.CreateLedgerRecord(ctx, db.CreateLedgerRecordParams{
		EntryID:       uuid.New().String(),
		WalletID:      senderWallet.WalletID,
		TransactionID: transactionID,
		Amount:        req.Amount,
		EntryType:     string(utils.Debit),
		BalanceAfter:  senderBalance,
	})
	if err != nil {
		return fmt.Errorf("failed to create debit ledger: %w", err)
	}

	// Credit ledger
	err = q.CreateLedgerRecord(ctx, db.CreateLedgerRecordParams{
		EntryID:       uuid.New().String(),
		WalletID:      receiverWallet.WalletID,
		TransactionID: transactionID,
		Amount:        req.Amount,
		EntryType:     string(utils.Credit),
		BalanceAfter:  receiverBalance,
	})
	if err != nil {
		return fmt.Errorf("failed to create credit ledger: %w", err)
	}

	// PENDING -> PROCESSED
	rows, err := q.UpdateTransferStatus(ctx, db.UpdateTransferStatusParams{
		Status:        string(utils.PROCESSED),
		TransactionID: transactionID,
	})
	if err != nil {
		return fmt.Errorf("failed to update transfer status: %w", err)
	}

	if rows != 1 {
		return fmt.Errorf("invalid transfer state transition")
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func retrySQLiteLock(ctx context.Context, operation func() error) error {
	for attempt := 0; attempt < maxSQLiteLockRetries; attempt++ {
		err := operation()
		if err == nil || !isSQLiteLockError(err) || attempt == maxSQLiteLockRetries-1 {
			return err
		}

		delay := initialSQLiteRetry * time.Duration(1<<attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func isSQLiteLockError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") ||
		strings.Contains(message, "database table is locked") ||
		strings.Contains(message, "database is busy")
}
