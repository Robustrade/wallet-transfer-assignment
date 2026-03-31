package service

import (
	"context"
	"errors"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/google/uuid"
)

var (
	ErrInvalidRequest    = errors.New("invalid request: check amount and wallet IDs")
	ErrInsufficientFunds = errors.New("insufficient funds")
)

type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type TransferService interface {
	Transfer(ctx context.Context, req TransferRequest) (*model.Transfer, error)
}

type transferService struct {
	repo repository.Repository
}

func NewTransferService(repo repository.Repository) TransferService {
	return &transferService{repo: repo}
}

func (s *transferService) Transfer(ctx context.Context, req TransferRequest) (*model.Transfer, error) {
	// 1. Prepare Transfer Model and Apply Domain Validation Rules
	now := time.Now().UTC()
	transfer := &model.Transfer{
		ID:             uuid.New().String(),
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         model.StatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := transfer.Validate(); err != nil {
		return nil, ErrInvalidRequest
	}

	// 2. Check Idempotency immediately cache read
	existing, err := s.repo.GetTransferByIdempotencyKey(ctx, req.IdempotencyKey)
	if err == nil {
		// Idempotent return: exactly the same response as the previous invocation.
		return existing, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}

	// 3. Delegate to repository for transactional processing

	err = s.repo.ExecuteTransferTx(ctx, transfer)
	if err != nil {
		if errors.Is(err, repository.ErrInsufficientFunds) {
			return nil, ErrInsufficientFunds
		}
		if errors.Is(err, repository.ErrIdempotencyConflict) {
			// Extremely highly concurrent requests hit the idempotency exact same time.
			// One succeeded, one got unique constraint violation. Just fetch the succeeded one.
			return s.repo.GetTransferByIdempotencyKey(ctx, req.IdempotencyKey)
		}
		return nil, err
	}

	return transfer, nil
}
