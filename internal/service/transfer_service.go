package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"
)

// TransferService is the thin orchestration layer between HTTP handlers and
// the repository. Business validation that doesn't need the DB lives in
// domain.CreateTransferRequest.Validate(); everything that does need the DB
// (wallet existence, balance, idempotency, locking) lives behind
// repository.TransferRepository so it can be done atomically.
type TransferService struct {
	repo   repository.TransferRepository
	logger *slog.Logger
}

func NewTransferService(repo repository.TransferRepository, logger *slog.Logger) *TransferService {
	if logger == nil {
		logger = slog.Default()
	}
	return &TransferService{repo: repo, logger: logger}
}

// CreateTransferResult is what the service hands back to the HTTP layer:
// enough to pick a status code and body without the handler knowing
// anything about idempotency internals.
type CreateTransferResult struct {
	Transfer       *domain.Transfer
	IsReplay       bool
	ConflictReason string // non-empty only when the request must be rejected
}

func (s *TransferService) CreateTransfer(ctx context.Context, req domain.CreateTransferRequest) (*CreateTransferResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	start := time.Now()
	result, err := s.repo.ExecuteTransfer(ctx, req)
	duration := time.Since(start)

	if err != nil {
		s.logger.Error("transfer execution failed",
			"idempotency_key", req.IdempotencyKey,
			"from_wallet_id", req.FromWalletID,
			"to_wallet_id", req.ToWalletID,
			"amount_cents", req.Amount,
			"duration_ms", duration.Milliseconds(),
			"error", err,
		)
		return nil, fmt.Errorf("executing transfer: %w", err)
	}

	switch result.Outcome {
	case repository.OutcomeConflict:
		s.logger.Warn("idempotency conflict",
			"idempotency_key", req.IdempotencyKey,
			"reason", result.ConflictReason,
		)
		return &CreateTransferResult{ConflictReason: result.ConflictReason}, nil

	case repository.OutcomeReplayed:
		s.logger.Info("idempotent replay",
			"idempotency_key", req.IdempotencyKey,
			"transfer_id", result.Transfer.ID,
		)
		return &CreateTransferResult{Transfer: result.Transfer, IsReplay: true}, nil

	default: // OutcomeExecuted
		s.logger.Info("transfer executed",
			"idempotency_key", req.IdempotencyKey,
			"transfer_id", result.Transfer.ID,
			"from_wallet_id", req.FromWalletID,
			"to_wallet_id", req.ToWalletID,
			"amount_cents", req.Amount,
			"status", result.Transfer.Status,
			"duration_ms", duration.Milliseconds(),
		)
		return &CreateTransferResult{Transfer: result.Transfer}, nil
	}
}
