package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// TransferUseCase is the transfer API offered to transports. TransferService
// implements it; decorators such as LoggingTransfers wrap it.
type TransferUseCase interface {
	CreateTransfer(ctx context.Context, req domain.TransferRequest) (CreateTransferResult, error)
	GetTransfer(ctx context.Context, id uuid.UUID) (domain.Transfer, error)
}

var (
	_ TransferUseCase = (*TransferService)(nil)
	_ TransferUseCase = (*LoggingTransfers)(nil)
)

// LoggingTransfers decorates a TransferUseCase with structured outcome logs.
// Observability stays out of the transfer workflow, and further cross-cutting
// concerns (metrics, tracing) can be added as more decorators without editing it.
type LoggingTransfers struct {
	next   TransferUseCase
	logger *slog.Logger
}

// NewLoggingTransfers wraps next so every transfer outcome is logged.
func NewLoggingTransfers(next TransferUseCase, logger *slog.Logger) *LoggingTransfers {
	return &LoggingTransfers{next: next, logger: logger}
}

// CreateTransfer delegates to the wrapped use case and logs the outcome.
func (l *LoggingTransfers) CreateTransfer(ctx context.Context, req domain.TransferRequest) (CreateTransferResult, error) {
	res, err := l.next.CreateTransfer(ctx, req)
	if err != nil {
		l.logFailure(ctx, req, err)
		return res, err
	}
	l.logger.InfoContext(ctx, "transfer completed",
		slog.String("transfer_id", res.Transfer.ID.String()),
		slog.String("idempotency_key", req.IdempotencyKey),
		slog.String("status", string(res.Transfer.Status)),
		slog.String("failure_reason", string(res.Transfer.FailureReason)),
		slog.Int64("amount", req.Amount),
		slog.Bool("replayed", res.Replayed),
	)
	return res, nil
}

// GetTransfer delegates to the wrapped use case.
func (l *LoggingTransfers) GetTransfer(ctx context.Context, id uuid.UUID) (domain.Transfer, error) {
	return l.next.GetTransfer(ctx, id)
}

// logFailure logs client errors at INFO, retryable contention at WARN and
// everything else at ERROR, so alerts fire only on what needs a human.
func (l *LoggingTransfers) logFailure(ctx context.Context, req domain.TransferRequest, err error) {
	attrs := []any{
		slog.String("idempotency_key", req.IdempotencyKey),
		slog.String("from_wallet_id", req.FromWalletID),
		slog.String("to_wallet_id", req.ToWalletID),
		slog.Int64("amount", req.Amount),
		slog.String("error", err.Error()),
	}
	var verr *domain.ValidationError
	switch {
	case errors.As(err, &verr),
		errors.Is(err, domain.ErrWalletNotFound),
		errors.Is(err, domain.ErrIdempotencyKeyReused):
		l.logger.InfoContext(ctx, "transfer rejected", attrs...)
	case errors.Is(err, domain.ErrBusy):
		l.logger.WarnContext(ctx, "transfer busy, client may retry", attrs...)
	default:
		l.logger.ErrorContext(ctx, "transfer failed unexpectedly", attrs...)
	}
}
