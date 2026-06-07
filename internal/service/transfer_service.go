package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Saket121/wallet-transfer-assignment/internal/domain"
	"github.com/Saket121/wallet-transfer-assignment/internal/repository"
)

type TransferService struct {
	repo   repository.TransferRepository
	logger *slog.Logger
}

func NewTransferService(repo repository.TransferRepository, logger *slog.Logger) *TransferService {
	return &TransferService{
		repo:   repo,
		logger: logger,
	}
}

type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type TransferResult struct {
	Transfer   *domain.Transfer
	Idempotent bool // true if this was a duplicate request
}

func (s *TransferService) CreateTransfer(ctx context.Context, req TransferRequest) (*TransferResult, error) {
	transfer := &domain.Transfer{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	}

	// Validate the request
	if err := transfer.Validate(); err != nil {
		return nil, fmt.Errorf("validation error: %w", err)
	}

	// Check for existing transfer with the same idempotency key
	existing, err := s.repo.FindTransferByIdempotencyKey(ctx, req.IdempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("checking idempotency: %w", err)
	}
	if existing != nil {
		s.logger.Info("idempotent request detected",
			"idempotencyKey", req.IdempotencyKey,
			"transferId", existing.ID,
		)
		return &TransferResult{Transfer: existing, Idempotent: true}, nil
	}

	// Execute the transfer
	result, err := s.repo.ExecuteTransfer(ctx, transfer)
	if err != nil {
		return nil, fmt.Errorf("executing transfer: %w", err)
	}

	s.logger.Info("transfer completed",
		"transferId", result.ID,
		"status", result.Status,
		"fromWallet", result.FromWalletID,
		"toWallet", result.ToWalletID,
		"amount", result.Amount,
	)

	return &TransferResult{Transfer: result, Idempotent: false}, nil
}
