package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/rohitkumar27/wallet-transfer-assignment/internal/domain"
	"github.com/rohitkumar27/wallet-transfer-assignment/internal/repository"
)

const defaultWaitTimeout = 3 * time.Second

type TransferService struct {
	db          *sql.DB
	wallets     *repository.WalletRepo
	transfers   *repository.TransferRepo
	ledger      *repository.LedgerRepo
	idempotency *repository.IdempotencyRepo
	logger      *slog.Logger
	waitTimeout time.Duration
}

func NewTransferService(db *sql.DB, logger *slog.Logger) *TransferService {
	return &TransferService{
		db:          db,
		wallets:     repository.NewWalletRepo(),
		transfers:   repository.NewTransferRepo(),
		ledger:      repository.NewLedgerRepo(),
		idempotency: repository.NewIdempotencyRepo(),
		logger:      logger,
		waitTimeout: defaultWaitTimeout,
	}
}

// CreateTransfer executes (or replays) a wallet transfer. See DESIGN.md for
// the full idempotency and locking rationale; in short: the whole request
// runs in one DB transaction, a row lock on the idempotency record is used
// to make concurrent duplicates wait for the original instead of racing
// it, and both wallets are locked in id order to prevent deadlocks.
func (s *TransferService) CreateTransfer(ctx context.Context, req domain.TransferRequest) (*Result, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	fingerprint := req.Fingerprint()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	won, err := s.idempotency.TryInsert(ctx, tx, req.IdempotencyKey, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("claim idempotency key: %w", err)
	}

	rec, err := s.idempotency.LockForUpdate(ctx, tx, req.IdempotencyKey, s.waitTimeout)
	if err != nil {
		if errors.Is(err, repository.ErrWaitTimeout) {
			return nil, domain.ErrProcessingTimeout
		}
		return nil, fmt.Errorf("lock idempotency record: %w", err)
	}

	if !won {
		if rec.RequestFingerprint != fingerprint {
			return nil, domain.ErrIdempotencyConflict
		}
		// rec.Status is guaranteed COMPLETED here: we only acquired this
		// row's lock after the transaction that owned it committed.
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit replay: %w", err)
		}
		committed = true
		status := 0
		if rec.ResponseStatus != nil {
			status = *rec.ResponseStatus
		}
		s.logger.Info("idempotent replay", "idempotencyKey", req.IdempotencyKey)
		return &Result{StatusCode: status, Body: rec.ResponseBody}, nil
	}

	result, transferID, err := s.executeTransfer(ctx, tx, req)
	if err != nil {
		return nil, fmt.Errorf("execute transfer: %w", err)
	}

	if err := s.idempotency.Complete(ctx, tx, req.IdempotencyKey, transferID, result.StatusCode, result.Body); err != nil {
		return nil, fmt.Errorf("complete idempotency record: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transfer: %w", err)
	}
	committed = true

	s.logger.Info("transfer request handled", "idempotencyKey", req.IdempotencyKey, "statusCode", result.StatusCode)
	return result, nil
}

// executeTransfer performs the wallet lookups, balance check, and ledger
// writes. It returns the response to send and, if a transfer row was
// created, its id (nil when the request failed lookup before a transfer
// could legally be created, since transfers.from/to_wallet_id are foreign
// keys into wallets).
func (s *TransferService) executeTransfer(ctx context.Context, tx *sql.Tx, req domain.TransferRequest) (*Result, *uuid.UUID, error) {
	wallets, err := s.wallets.LockAndGet(ctx, tx, req.FromWalletID, req.ToWalletID)
	if err != nil {
		return nil, nil, fmt.Errorf("lock wallets: %w", err)
	}

	from, ok := wallets[req.FromWalletID]
	if !ok {
		res, err := errorResult(404, fmt.Sprintf("wallet not found: %s", req.FromWalletID))
		return res, nil, err
	}
	to, ok := wallets[req.ToWalletID]
	if !ok {
		res, err := errorResult(404, fmt.Sprintf("wallet not found: %s", req.ToWalletID))
		return res, nil, err
	}

	transfer := domain.NewTransfer(req.FromWalletID, req.ToWalletID, req.Amount)
	if err := s.transfers.Create(ctx, tx, transfer); err != nil {
		return nil, nil, fmt.Errorf("create transfer: %w", err)
	}

	if from.Balance < req.Amount {
		if err := transfer.MarkFailed("insufficient funds"); err != nil {
			return nil, nil, err
		}
		if err := s.transfers.UpdateStatus(ctx, tx, transfer); err != nil {
			return nil, nil, fmt.Errorf("update transfer status: %w", err)
		}
		res, err := transferResult(409, transfer)
		return res, &transfer.ID, err
	}

	entries := domain.LedgerEntriesForTransfer(transfer)
	if err := s.ledger.InsertEntries(ctx, tx, entries); err != nil {
		return nil, nil, fmt.Errorf("insert ledger entries: %w", err)
	}
	if err := s.wallets.UpdateBalance(ctx, tx, from.ID, from.Balance-req.Amount); err != nil {
		return nil, nil, fmt.Errorf("debit wallet: %w", err)
	}
	if err := s.wallets.UpdateBalance(ctx, tx, to.ID, to.Balance+req.Amount); err != nil {
		return nil, nil, fmt.Errorf("credit wallet: %w", err)
	}
	if err := transfer.MarkProcessed(); err != nil {
		return nil, nil, err
	}
	if err := s.transfers.UpdateStatus(ctx, tx, transfer); err != nil {
		return nil, nil, fmt.Errorf("update transfer status: %w", err)
	}

	res, err := transferResult(201, transfer)
	return res, &transfer.ID, err
}
