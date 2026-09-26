package service

import (
	"context"
	"fmt"
	"strings"

	"wallet-transfer-assignment/internal/domain"
)

type Repository interface {
	CreateWallet(context.Context, string, int64) (domain.Wallet, error)
	GetWallet(context.Context, string) (domain.Wallet, error)
	ListTransfers(context.Context, string, int) ([]domain.Transfer, error)
	ExecuteTransfer(context.Context, domain.TransferRequest) (domain.TransferResult, error)
}

type Service struct {
	repository Repository
}

func New(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) CreateWallet(ctx context.Context, walletID string, openingBalance int64) (domain.Wallet, error) {
	if strings.TrimSpace(walletID) == "" || openingBalance < 0 {
		return domain.Wallet{}, fmt.Errorf("%w: wallet ID and non-negative opening balance are required", domain.ErrInvalidRequest)
	}
	return s.repository.CreateWallet(ctx, walletID, openingBalance)
}

func (s *Service) GetWallet(ctx context.Context, walletID string) (domain.Wallet, error) {
	if strings.TrimSpace(walletID) == "" {
		return domain.Wallet{}, fmt.Errorf("%w: wallet ID is required", domain.ErrInvalidRequest)
	}
	return s.repository.GetWallet(ctx, walletID)
}

func (s *Service) ListTransfers(ctx context.Context, walletID string, limit int) ([]domain.Transfer, error) {
	if strings.TrimSpace(walletID) == "" {
		return nil, fmt.Errorf("%w: wallet ID is required", domain.ErrInvalidRequest)
	}
	return s.repository.ListTransfers(ctx, walletID, limit)
}

func (s *Service) Transfer(ctx context.Context, request domain.TransferRequest) (domain.TransferResult, error) {
	if strings.TrimSpace(request.IdempotencyKey) == "" ||
		strings.TrimSpace(request.FromWalletID) == "" ||
		strings.TrimSpace(request.ToWalletID) == "" || request.Amount <= 0 {
		return domain.TransferResult{}, fmt.Errorf("%w: idempotency key, wallet IDs, and positive amount are required", domain.ErrInvalidRequest)
	}
	if request.FromWalletID == request.ToWalletID {
		return domain.TransferResult{}, fmt.Errorf("%w: source and destination wallets must differ", domain.ErrInvalidRequest)
	}
	return s.repository.ExecuteTransfer(ctx, request)
}
