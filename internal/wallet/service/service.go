package service

import (
	"context"
	"wallet-transfer/internal/transfer/domain"
	walletdomain "wallet-transfer/internal/wallet/domain"
)

type Repository interface {
	Create(context.Context, walletdomain.CreateRequest) (walletdomain.Wallet, error)
	Get(context.Context, string) (walletdomain.Wallet, error)
	List(context.Context) ([]walletdomain.Wallet, error)
	UpdateBalance(context.Context, string, int64) (walletdomain.Wallet, error)
	BalanceHistory(context.Context, string) ([]walletdomain.BalanceHistory, error)
	ListTransfers(context.Context, string) ([]domain.Transfer, error)
}

type Service struct{ repository Repository }

func New(repository Repository) *Service { return &Service{repository: repository} }
func (s *Service) Create(ctx context.Context, request walletdomain.CreateRequest) (walletdomain.Wallet, error) {
	if err := request.Validate(); err != nil {
		return walletdomain.Wallet{}, walletdomain.ErrInvalidRequest
	}
	return s.repository.Create(ctx, request)
}
func (s *Service) Get(ctx context.Context, id string) (walletdomain.Wallet, error) {
	if id == "" {
		return walletdomain.Wallet{}, walletdomain.ErrInvalidRequest
	}
	return s.repository.Get(ctx, id)
}

func (s *Service) List(ctx context.Context) ([]walletdomain.Wallet, error) {
	return s.repository.List(ctx)
}

func (s *Service) UpdateBalance(ctx context.Context, id string, balance int64) (walletdomain.Wallet, error) {
	if id == "" || balance < 0 {
		return walletdomain.Wallet{}, walletdomain.ErrInvalidRequest
	}
	return s.repository.UpdateBalance(ctx, id, balance)
}

func (s *Service) BalanceHistory(ctx context.Context, id string) ([]walletdomain.BalanceHistory, error) {
	if id == "" {
		return nil, walletdomain.ErrInvalidRequest
	}
	return s.repository.BalanceHistory(ctx, id)
}
func (s *Service) ListTransfers(ctx context.Context, id string) ([]domain.Transfer, error) {
	if id == "" {
		return nil, walletdomain.ErrInvalidRequest
	}
	return s.repository.ListTransfers(ctx, id)
}
