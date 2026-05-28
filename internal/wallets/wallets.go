package wallets

import (
	"context"
	"errors"
)

type Wallet struct {
	ID      string
	Balance int64
}

var (
	ErrNotFound = errors.New("wallet not found")
)

type Repository interface {
	Create(ctx context.Context, initialBalance int64) (Wallet, error)
	Get(ctx context.Context, walletID string) (Wallet, error)
	Exists(ctx context.Context, walletID string) (bool, error)
}

type Service struct {
	repo Repository
}

type ServiceDeps struct {
	Wallets Repository
}

func NewService(deps ServiceDeps) *Service {
	return &Service{repo: deps.Wallets}
}

func (s *Service) Create(ctx context.Context, initialBalance int64) (Wallet, error) {
	return s.repo.Create(ctx, initialBalance)
}

func (s *Service) Get(ctx context.Context, walletID string) (Wallet, error) {
	return s.repo.Get(ctx, walletID)
}
