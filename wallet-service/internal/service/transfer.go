package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-service/internal/database"
	"wallet-service/internal/domain"
	"wallet-service/internal/repository"
)

var (
	ErrUnavailable   = errors.New("temporarily unavailable")
	ErrCommitUnknown = errors.New("commit outcome unknown")
)

type Service struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

type requestIDKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// Transfer returns saved responses for success and business rejections. Errors
// return an empty result; callers map them to 400/409/503/500 as documented.
func (s *Service) Transfer(ctx context.Context, req domain.Request) (result repository.Result, err error) {
	started, outcome, transferID := time.Now(), "", ""
	defer func() {
		if err != nil {
			result = repository.Result{}
			err = classifyError(err)
		}
		logTransfer(ctx, started, outcome, transferID, result, err)
	}()
	if err := domain.ValidateRequest(req); err != nil {
		return repository.Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := database.BeginReadCommitted(ctx, s.pool)
	if err != nil {
		return repository.Result{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() {
		// Cancellation must not prevent cleanup. pgx closes the connection if
		// rollback fails; after commit it returns the harmless ErrTxClosed.
		cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
	repo := repository.New(tx)
	payload := domain.Payload{FromWalletID: req.FromWalletID, ToWalletID: req.ToWalletID, Amount: req.Amount}
	record, err := repo.ClaimOrLoad(ctx, req.IdempotencyKey, payload)
	if err != nil {
		return repository.Result{}, err
	}
	if record != nil {
		if record.Payload != payload {
			return repository.Result{}, domain.ErrIdempotencyConflict
		}
		outcome = "replayed"
		return record.Result, nil
	}

	from, to, err := repo.LockWallets(ctx, req.FromWalletID, req.ToWalletID)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			return saveAndCommit(ctx, tx, req.IdempotencyKey, domain.Transfer{}, domain.ErrWalletNotFound)
		}
		return repository.Result{}, err
	}
	if err := domain.ValidateWalletPair(from, to); err != nil {
		return saveAndCommit(ctx, tx, req.IdempotencyKey, domain.Transfer{}, domain.Error(err.Error()))
	}
	transfer, err := repo.CreatePending(ctx, payload, from.Currency)
	if err != nil {
		return repository.Result{}, err
	}
	transferID = transfer.ID
	transfer.Status = domain.Processed
	var failure domain.Error
	switch balanceErr := domain.CheckTransferBalances(from, to, req.Amount); balanceErr {
	case nil:
		from.Balance -= req.Amount
		to.Balance += req.Amount
		if err := repo.UpdateBalances(ctx, from, to); err != nil {
			return repository.Result{}, err
		}
		if err := repo.InsertLedgerPair(ctx, transfer.ID); err != nil {
			return repository.Result{}, err
		}
	case domain.ErrInsufficientFunds, domain.ErrBalanceOverflow:
		transfer.Status = domain.Failed
		failure = domain.Error(balanceErr.Error())
	default:
		return repository.Result{}, fmt.Errorf("invalid stored balances: %w", repository.ErrInvariant)
	}
	if err := repo.CompleteTransfer(ctx, transfer.ID, transfer.Status, string(failure)); err != nil {
		return repository.Result{}, err
	}
	return saveAndCommit(ctx, tx, req.IdempotencyKey, transfer, failure)
}

func makeResult(transfer domain.Transfer, code domain.Error) (repository.Result, error) {
	httpStatus := 201
	switch code {
	case "":
	case domain.ErrWalletNotFound:
		httpStatus = 404
	case domain.ErrCurrencyMismatch, domain.ErrUnsupportedCurrency, domain.ErrInsufficientFunds, domain.ErrBalanceOverflow:
		httpStatus = 422
	default:
		return repository.Result{}, fmt.Errorf("unexpected business error: %w", code)
	}
	var errorBody map[string]domain.Error
	if code != "" {
		errorBody = map[string]domain.Error{"code": code}
	}
	body, err := json.Marshal(struct {
		TransferID string                  `json:"transferId,omitempty"`
		Status     domain.Status           `json:"status,omitempty"`
		Error      map[string]domain.Error `json:"error,omitempty"`
	}{transfer.ID, transfer.Status, errorBody})
	if err != nil {
		return repository.Result{}, fmt.Errorf("encode response: %w", err)
	}
	return repository.Result{TransferID: transfer.ID, HTTPStatus: httpStatus, Body: string(body)}, nil
}

func saveAndCommit(ctx context.Context, tx pgx.Tx, key string, transfer domain.Transfer, code domain.Error) (repository.Result, error) {
	result, err := makeResult(transfer, code)
	if err != nil {
		return repository.Result{}, err
	}
	if err := repository.New(tx).SaveResult(ctx, key, result); err != nil {
		return repository.Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		var pgErr *pgconn.PgError
		rejected := errors.As(err, &pgErr) && (pgErr.SeverityUnlocalized == "ERROR" || pgErr.Severity == "ERROR")
		if errors.Is(err, pgx.ErrTxCommitRollback) || rejected {
			return repository.Result{}, err // PostgreSQL explicitly rejected commit.
		}
		// A missing acknowledgment cannot tell us whether money moved.
		return repository.Result{}, fmt.Errorf("%w: %w", ErrCommitUnknown, err)
	}
	return result, nil
}

func classifyError(err error) error {
	if errors.Is(err, ErrUnavailable) {
		return err
	}
	var pgErr *pgconn.PgError
	var netErr net.Error
	unavailable := errors.Is(err, ErrCommitUnknown) || errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &netErr) || pgconn.SafeToRetry(err)
	if errors.As(err, &pgErr) {
		unavailable = unavailable || strings.HasPrefix(pgErr.Code, "08") ||
			strings.HasPrefix(pgErr.Code, "40") || strings.HasPrefix(pgErr.Code, "53") ||
			strings.HasPrefix(pgErr.Code, "57") || pgErr.Code == "55P03"
	}
	if unavailable {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return err
}

func logTransfer(ctx context.Context, started time.Time, outcome, transferID string, result repository.Result, err error) {
	code, level := "", slog.LevelInfo
	if err != nil {
		outcome, code, level = "rolled_back", "INTERNAL_ERROR", slog.LevelError
		switch {
		case errors.Is(err, ErrCommitUnknown):
			outcome, code = "unknown", "TEMPORARILY_UNAVAILABLE"
		case errors.Is(err, ErrUnavailable):
			code = "TEMPORARILY_UNAVAILABLE"
		case errors.Is(err, domain.ErrIdempotencyConflict):
			outcome, code, level = "conflict", "IDEMPOTENCY_CONFLICT", slog.LevelInfo
		case errors.Is(err, domain.ErrInvalidRequest):
			outcome, code, level = "rejected", "INVALID_REQUEST", slog.LevelInfo
		case errors.Is(err, domain.ErrSelfTransfer):
			outcome, code, level = "rejected", "SELF_TRANSFER", slog.LevelInfo
		}
	} else {
		if outcome != "replayed" {
			outcome = "processed"
			if result.HTTPStatus != 201 {
				outcome = "failed"
			}
		}
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal([]byte(result.Body), &body)
		code, transferID = body.Error.Code, result.TransferID
	}
	requestID, _ := ctx.Value(requestIDKey{}).(string)
	attrs := []any{"request_id", requestID, "outcome", outcome, "error_code", code, "elapsed_ms", time.Since(started).Milliseconds()}
	if transferID != "" {
		attrs = append(attrs, "transfer_id", transferID)
	}
	if result.HTTPStatus != 0 {
		attrs = append(attrs, "http_status", result.HTTPStatus)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		attrs = append(attrs, "sqlstate", pgErr.Code)
	}
	slog.Log(ctx, level, "transfer", attrs...)
}
