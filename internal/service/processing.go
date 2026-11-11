package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"gorm.io/gorm"

	"wallet-transfer-assignment/internal/appErrors"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository/postgres"
)

type TransferService struct {
	db               *gorm.DB
	operationTimeout time.Duration
	maxAmount        int64
}

func NewTransferService(db *gorm.DB, operationTimeout time.Duration, maxAmount int64) *TransferService {
	if operationTimeout <= 0 {
		operationTimeout = 5 * time.Second
	}
	return &TransferService{db: db, operationTimeout: operationTimeout, maxAmount: maxAmount}
}

// ProcessTransfer processes an existing reservation. No result is returned until
// commit is confirmed; the caller must resolve uncertain outcomes by retrying.
func (s *TransferService) ProcessTransfer(ctx context.Context, id string) (TransferResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.operationTimeout)
	defer cancel()
	tx := s.db.WithContext(ctx).Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if tx.Error != nil {
		return TransferResult{}, processingError(tx.Error)
	}
	defer func() { _ = tx.Rollback().Error }()
	if err := postgres.SetTransactionTimeouts(tx, min(s.operationTimeout, 2*time.Second), min(s.operationTimeout, 4*time.Second)); err != nil {
		return TransferResult{}, processingError(err)
	}
	transfer, err := postgres.LockTransfer(tx, id)
	if err != nil {
		return TransferResult{}, processingError(err)
	}
	if transfer.Status == domain.Processed || transfer.Status == domain.Failed {
		return replayResult(tx, id)
	}
	if transfer.Status != domain.Pending {
		return TransferResult{}, appErrors.ErrInvalidTransition
	}
	result, err := processPendingTransfer(tx, transfer)
	if err != nil {
		return TransferResult{}, err
	}
	if err := tx.Commit().Error; err != nil {
		// The connection may have failed after PostgreSQL committed. Never return
		// a terminal result or convert this into a persisted business failure.
		return TransferResult{}, fmt.Errorf("%w: commit transfer: %w", appErrors.ErrServiceUnavailable, err)
	}
	return result, nil
}

// processPendingTransfer applies financial changes and saves the response in the
// caller-owned transaction. ProcessTransfer remains responsible for committing it.
func processPendingTransfer(tx *gorm.DB, transfer domain.Transfer) (TransferResult, error) {
	source, destination, err := lockWallets(tx, transfer)
	if err != nil {
		return TransferResult{}, processingError(err)
	}
	if source.Balance < transfer.Amount {
		transfer.FailureReason = domain.ReasonInsufficientFunds
		if err := transfer.Transition(domain.Failed); err != nil {
			return TransferResult{}, err
		}
	} else {
		if destination.Balance > math.MaxInt64-transfer.Amount {
			return TransferResult{}, appErrors.ErrBalanceOverflow
		}
		if err := moveMoney(tx, transfer, source.Balance, destination.Balance); err != nil {
			return TransferResult{}, processingError(err)
		}
		if err := transfer.Transition(domain.Processed); err != nil {
			return TransferResult{}, err
		}
	}
	result, err := terminalResult(transfer)
	if err != nil {
		return TransferResult{}, err
	}
	if err := postgres.CompleteTransfer(tx, transfer, result.StatusCode, result.Body); err != nil {
		return TransferResult{}, processingError(err)
	}
	return result, nil
}

func lockWallets(tx *gorm.DB, transfer domain.Transfer) (domain.Wallet, domain.Wallet, error) {
	firstID, secondID := transfer.FromWalletID, transfer.ToWalletID
	if firstID > secondID {
		firstID, secondID = secondID, firstID
	}
	first, err := postgres.LockWallet(tx, firstID)
	if err != nil {
		return domain.Wallet{}, domain.Wallet{}, err
	}
	second, err := postgres.LockWallet(tx, secondID)
	if err != nil {
		return domain.Wallet{}, domain.Wallet{}, err
	}
	if first.ID == transfer.FromWalletID {
		return first, second, nil
	}
	return second, first, nil
}

func moveMoney(tx *gorm.DB, transfer domain.Transfer, sourceBalance, destinationBalance int64) error {
	if err := postgres.UpdateWalletBalance(tx, transfer.FromWalletID, sourceBalance-transfer.Amount); err != nil {
		return err
	}
	if err := postgres.UpdateWalletBalance(tx, transfer.ToWalletID, destinationBalance+transfer.Amount); err != nil {
		return err
	}
	entries := []domain.LedgerEntry{
		{TransferID: transfer.ID, WalletID: transfer.FromWalletID, Type: domain.Debit, Amount: transfer.Amount},
		{TransferID: transfer.ID, WalletID: transfer.ToWalletID, Type: domain.Credit, Amount: transfer.Amount},
	}
	return postgres.InsertLedgerEntries(tx, entries)
}

func terminalResult(transfer domain.Transfer) (TransferResult, error) {
	status := http.StatusCreated
	if transfer.Status == domain.Failed {
		status = http.StatusUnprocessableEntity
	}
	body, err := json.Marshal(TransferResponse{TransferID: transfer.ID, Status: transfer.Status, FailureReason: transfer.FailureReason})
	if err != nil {
		return TransferResult{}, fmt.Errorf("encode transfer response: %w", err)
	}
	return TransferResult{StatusCode: status, Body: body}, nil
}

func processingError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %w", appErrors.ErrTransferTemporarilyUnavailable, err)
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case "55P03", "57014", "40P01", "40001":
			return fmt.Errorf("%w: %w", appErrors.ErrTransferTemporarilyUnavailable, err)
		case "57P01", "57P02", "57P03": // shutdown, crash, or temporarily refusing connections
			return fmt.Errorf("%w: %w", appErrors.ErrServiceUnavailable, err)
		}
		if strings.HasPrefix(pgError.Code, "08") {
			return fmt.Errorf("%w: %w", appErrors.ErrServiceUnavailable, err)
		}
	}
	var networkError net.Error
	if errors.Is(err, driver.ErrBadConn) ||
		errors.Is(err, sql.ErrConnDone) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.As(err, &networkError) {
		return fmt.Errorf("%w: %w", appErrors.ErrServiceUnavailable, err)
	}
	return fmt.Errorf("process transfer: %w", err)
}
