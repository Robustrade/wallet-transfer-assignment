package service

import (
	"context"

	"github.com/robustrade/wallet-transfer/internal/domain"
)

type Repository interface {
	CreateTransfer(context.Context, domain.TransferRequest) (domain.TransferResult, error)
	GetWallet(context.Context, string) (domain.Wallet, error)
}

type Transfers struct{ repository Repository }

func NewTransfers(repository Repository) *Transfers { return &Transfers{repository: repository} }

func (s *Transfers) Create(ctx context.Context, request domain.TransferRequest) (domain.TransferResult, error) {
	if err := request.Validate(); err != nil {
		return domain.TransferResult{}, err
	}
	return s.repository.CreateTransfer(ctx, request)
}

func (s *Transfers) Wallet(ctx context.Context, id string) (domain.Wallet, error) {
	if id == "" {
		return domain.Wallet{}, domain.ErrInvalidRequest
	}
	return s.repository.GetWallet(ctx, id)
}
