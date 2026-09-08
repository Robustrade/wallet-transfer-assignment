package service

import (
	"context"
	"errors"
	"wallet-transfer/internal/transfer/domain"
)

type Repository interface {
	Create(context.Context, domain.CreateRequest) (domain.Transfer, error)
	Get(context.Context, string) (domain.Transfer, error)
	ListAll(context.Context) ([]domain.Transfer, error)
}

type Service struct{ repository Repository }

func New(repository Repository) *Service { return &Service{repository: repository} }
func (s *Service) Create(ctx context.Context, request domain.CreateRequest) (domain.Transfer, error) {
	if err := request.Validate(); err != nil {
		return domain.Transfer{}, err
	}
	return s.repository.Create(ctx, request)
}
func (s *Service) ListAll(ctx context.Context) ([]domain.Transfer, error) {
	return s.repository.ListAll(ctx)
}

func (s *Service) Get(ctx context.Context, id string) (domain.Transfer, error) {
	if id == "" {
		return domain.Transfer{}, domain.ErrInvalidRequest
	}
	return s.repository.Get(ctx, id)
}
func IsBusinessFailure(err error) bool {
	return errors.Is(err, domain.ErrInsufficientFunds) || errors.Is(err, domain.ErrWalletNotFound)
}
