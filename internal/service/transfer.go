package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

var (
	ErrInvalidRequest      = errors.New("invalid transfer request")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrIdempotencyConflict = errors.New("idempotency key used for different request")
	ErrIncompleteResult    = errors.New("idempotency result is incomplete")
)

type Request struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type Service struct {
	work   repository.UnitOfWork
	logger *slog.Logger
}

func New(work repository.UnitOfWork, logger *slog.Logger) *Service {
	if logger == nil {
		panic("service logger is required")
	}
	return &Service{work: work, logger: logger}
}

type response struct {
	TransferID   string                `json:"transferId"`
	State        domain.TransferStatus `json:"state"`
	FromWalletID string                `json:"fromWalletId,omitempty"`
	ToWalletID   string                `json:"toWalletId,omitempty"`
	Amount       int64                 `json:"amount,omitempty"`
	Error        *businessError        `json:"error,omitempty"`
}
type businessError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Transfer returns a terminal result only after the whole unit of work commits.
// A committed business rejection is a result, not a callback error. Any returned
// error discards the tentative response; commit errors may have unknown outcomes.
func (s *Service) Transfer(ctx context.Context, req Request) (result repository.Result, err error) {
	started := time.Now()
	replayed := false
	transferID := ""
	logEvent := func(level slog.Level, event, status string, attrs ...any) {
		fields := []any{"event", event, "transfer_id", transferID, "idempotency_key", req.IdempotencyKey, "from_wallet_id", req.FromWalletID, "to_wallet_id", req.ToWalletID, "status", status}
		s.logger.Log(ctx, level, event, append(fields, attrs...)...)
	}
	defer func() {
		if failure := recover(); failure != nil {
			logEvent(slog.LevelError, "transaction_failed", "UNKNOWN", "duration", time.Since(started), "cause", "panic")
			panic(failure)
		}
		event, level, status := "transfer_committed", slog.LevelInfo, "UNKNOWN"
		if err != nil {
			event, level = "transaction_failed", slog.LevelError
			if errors.Is(err, ErrInvalidRequest) {
				event, level = "request_rejected", slog.LevelWarn
			}
			if errors.Is(err, ErrIdempotencyConflict) {
				event, level = "idempotency_conflict", slog.LevelWarn
			}
		} else {
			var stored response
			if json.Unmarshal([]byte(result.Body), &stored) == nil {
				status = string(stored.State)
			}
			if replayed {
				event = "duplicate_idempotent_request"
			}
		}
		// A commit error may have an unknown outcome. Never claim rollback or a
		// persisted FAILED state merely because WithinTransaction returned an error.
		logEvent(level, event, status, "duration", time.Since(started), "retryable_transaction", errors.Is(err, repository.ErrRetryableTransaction))
	}()
	if strings.ContainsRune(req.FromWalletID, 0) || strings.ContainsRune(req.ToWalletID, 0) || strings.ContainsRune(req.IdempotencyKey, 0) || !utf8.ValidString(req.FromWalletID) || !utf8.ValidString(req.ToWalletID) || !utf8.ValidString(req.IdempotencyKey) || req.Amount <= 0 || strings.TrimSpace(req.FromWalletID) == "" || strings.TrimSpace(req.ToWalletID) == "" || req.FromWalletID == req.ToWalletID {
		return repository.Result{}, ErrInvalidRequest
	}
	// Fixed ordered tuple, integer amount, no raw HTTP JSON or ambiguous concatenation.
	canonical, err := json.Marshal([3]any{req.FromWalletID, req.ToWalletID, req.Amount})
	if err != nil {
		return repository.Result{}, fmt.Errorf("encode fingerprint: %w", err)
	}
	sum := sha256.Sum256(canonical)
	fingerprint := hex.EncodeToString(sum[:])
	var entropy [16]byte
	if _, err = rand.Read(entropy[:]); err != nil {
		return repository.Result{}, fmt.Errorf("generate transfer ID: %w", err)
	}
	transferID = hex.EncodeToString(entropy[:])
	logEvent(slog.LevelInfo, "request_accepted", "PENDING")
	var pending repository.Result
	err = s.work.WithinTransaction(ctx, func(ctx context.Context, tx repository.Tx) error {
		at := time.Now().UTC()
		if req.IdempotencyKey != "" {
			reserved, err := tx.ReserveIdempotencyKey(ctx, req.IdempotencyKey, fingerprint, transferID, at)
			if err != nil {
				return err
			}
			if !reserved {
				record, err := tx.GetIdempotencyRecord(ctx, req.IdempotencyKey)
				if err != nil {
					return err
				}
				transferID = record.TransferID
				if record.Fingerprint != fingerprint {
					return ErrIdempotencyConflict
				}
				if record.Result == nil {
					return ErrIncompleteResult
				}
				pending = *record.Result
				transferID = record.TransferID
				replayed = true
				return nil
			}
		}
		from, to, err := tx.LockWallets(ctx, req.FromWalletID, req.ToWalletID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) && !errors.Is(err, repository.ErrTransactionFailed) {
				return fmt.Errorf("lock wallets: %w", ErrWalletNotFound)
			}
			return err
		}
		transfer, err := domain.NewTransfer(transferID, req.IdempotencyKey, req.FromWalletID, req.ToWalletID, req.Amount, at)
		if err != nil {
			return err
		}
		if err = tx.CreateTransfer(ctx, transfer); err != nil {
			return err
		}
		var rejection *businessError
		switch {
		case from.Balance < req.Amount:
			rejection = &businessError{"INSUFFICIENT_BALANCE", "Source wallet has insufficient balance"}
			logEvent(slog.LevelInfo, "insufficient_balance", "PENDING")
		case to.Balance > math.MaxInt64-req.Amount:
			rejection = &businessError{"BALANCE_OVERFLOW", "Destination balance would exceed the supported limit"}
		}
		body := response{TransferID: transferID}
		if rejection != nil {
			if err = tx.MarkTransferFailed(ctx, transferID, at); err != nil {
				return err
			}
			body.State = domain.TransferStatusFailed
			body.Error = rejection
			pending.StatusCode = 422
		} else {
			if err = tx.UpdateWalletBalance(ctx, from.ID, from.Balance-req.Amount, at); err != nil {
				return err
			}
			if err = tx.UpdateWalletBalance(ctx, to.ID, to.Balance+req.Amount, at); err != nil {
				return err
			}
			debit := domain.LedgerEntry{ID: transferID + "-debit", WalletID: from.ID, TransferID: transferID, Type: domain.LedgerEntryDebit, Amount: req.Amount, CreatedAt: at}
			if err = tx.CreateLedgerEntry(ctx, debit); err != nil {
				return err
			}
			credit := domain.LedgerEntry{ID: transferID + "-credit", WalletID: to.ID, TransferID: transferID, Type: domain.LedgerEntryCredit, Amount: req.Amount, CreatedAt: at}
			if err = tx.CreateLedgerEntry(ctx, credit); err != nil {
				return err
			}
			if err = tx.MarkTransferProcessed(ctx, transferID, at); err != nil {
				return err
			}
			body.State = domain.TransferStatusProcessed
			body.FromWalletID = from.ID
			body.ToWalletID = to.ID
			body.Amount = req.Amount
			pending.StatusCode = 201
		}
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode transfer result: %w", err)
		}
		pending.Body = string(encoded)
		if req.IdempotencyKey != "" {
			return tx.CompleteIdempotencyKey(ctx, req.IdempotencyKey, transferID, pending, at)
		}
		return nil
	})
	if err != nil {
		return repository.Result{}, fmt.Errorf("transfer: %w", err)
	}
	return pending, nil
}
