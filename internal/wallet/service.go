package wallet

import (
	"context"
	"strconv"

	"wallet-transfer-assignment/internal/api/dtos"
)

type WalletService interface {
	TransferFunds(ctx context.Context, req dtos.TransferRequest) (*Transaction, error)
	CreateWallet(ctx context.Context, req dtos.CreateWalletRequest) (*Wallet, error)
	GetWallet(ctx context.Context, walletId string) (*Wallet, error)
}

type walletService struct {
	repo WalletRepository
}

func NewService(repo WalletRepository) WalletService {
	return &walletService{repo: repo}
}

func (s *walletService) TransferFunds(ctx context.Context, req dtos.TransferRequest) (*Transaction, error) {
	// convert wallet ids to sortable int64 ids
	fromID, err := strconv.ParseInt(req.FromWalletId, 10, 64)
	if err != nil {
		return nil, err
	}
	toID, err := strconv.ParseInt(req.ToWalletId, 10, 64)
	if err != nil {
		return nil, err
	}

	return s.repo.Transfer(ctx, req.IdempotencyKey, fromID, toID, req.Amount)
}

// CreateWallet creates a new wallet with given id and initial balance
func (s *walletService) CreateWallet(ctx context.Context, req dtos.CreateWalletRequest) (*Wallet, error) {
	id, err := strconv.ParseInt(req.WalletId, 10, 64)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateWallet(ctx, id, req.InitialBalance); err != nil {
		return nil, err
	}
	return s.repo.GetWallet(ctx, id)
}

// GetWallet returns wallet by id
func (s *walletService) GetWallet(ctx context.Context, walletId string) (*Wallet, error) {
	id, err := strconv.ParseInt(walletId, 10, 64)
	if err != nil {
		return nil, err
	}
	return s.repo.GetWallet(ctx, id)
}
