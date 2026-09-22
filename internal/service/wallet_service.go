package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/repository"
)

// WalletService manages wallet lifecycle and read-side queries. It is kept
// separate from TransferService because wallet provisioning and balance
// lookups are not part of the transfer state machine.
type WalletService struct {
	uow   repository.UnitOfWork
	now   func() time.Time
	newID func() string
}

// NewWalletService constructs a WalletService backed by uow.
func NewWalletService(uow repository.UnitOfWork, opts ...Option) *WalletService {
	ts := NewTransferService(uow, opts...)
	return &WalletService{uow: uow, now: ts.now, newID: ts.newID}
}

// CreateWalletInput describes a new wallet to provision.
type CreateWalletInput struct {
	// ID is optional; a UUID is generated when empty.
	ID             string
	InitialBalance int64
}

// CreateWallet provisions a new wallet with an opening balance.
func (s *WalletService) CreateWallet(ctx context.Context, in CreateWalletInput) (*domain.Wallet, error) {
	if in.InitialBalance < 0 {
		return nil, domain.ErrInvalidAmount
	}
	id := in.ID
	if id == "" {
		id = uuid.NewString()
	}

	var wallet *domain.Wallet
	err := s.uow.Execute(ctx, func(ctx context.Context, repos repository.Repositories) error {
		now := s.now()
		w := domain.Wallet{ID: id, Balance: in.InitialBalance, CreatedAt: now, UpdatedAt: now}
		if err := repos.Wallets.Create(ctx, &w); err != nil {
			return err
		}
		wallet = &w
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("create wallet: %w", err)
	}
	return wallet, nil
}

// GetWallet returns a wallet's current state, including its balance.
func (s *WalletService) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	var wallet *domain.Wallet
	err := s.uow.Execute(ctx, func(ctx context.Context, repos repository.Repositories) error {
		w, err := repos.Wallets.Get(ctx, id)
		if err != nil {
			return err
		}
		wallet = w
		return nil
	})
	if err != nil {
		return nil, err
	}
	return wallet, nil
}

// ListTransferHistory returns every transfer touching walletID, most
// recent first.
func (s *WalletService) ListTransferHistory(ctx context.Context, walletID string) ([]domain.Transfer, error) {
	var transfers []domain.Transfer
	err := s.uow.Execute(ctx, func(ctx context.Context, repos repository.Repositories) error {
		if _, err := repos.Wallets.Get(ctx, walletID); err != nil {
			return err
		}
		list, err := repos.Transfers.ListByWallet(ctx, walletID)
		if err != nil {
			return err
		}
		transfers = list
		return nil
	})
	if err != nil {
		return nil, err
	}
	return transfers, nil
}
