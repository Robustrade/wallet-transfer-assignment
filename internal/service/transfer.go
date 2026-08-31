package service

import (
	"context"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/domain"
)

// TransferStore persists a complete transfer state transition.
type TransferStore interface {
	CreateTransfer(context.Context, domain.CreateTransferRequest) (domain.Transfer, error)
	Balance(context.Context, string) (int64, error)
	TransferHistory(context.Context, string, int) ([]domain.Transfer, error)
}

// TransferService enforces domain validation before delegating persistence.
type TransferService struct{ store TransferStore }

// NewTransferService creates the application service for transfer requests.
func NewTransferService(store TransferStore) *TransferService { return &TransferService{store: store} }

// CreateTransfer validates the request and delegates the atomic workflow to the store.
func (s *TransferService) CreateTransfer(ctx context.Context, req domain.CreateTransferRequest) (domain.Transfer, error) {
	if err := req.Validate(); err != nil {
		return domain.Transfer{}, err
	}
	return s.store.CreateTransfer(ctx, req)
}

// Balance validates a wallet identifier and returns its current stored balance.
func (s *TransferService) Balance(ctx context.Context, walletID string) (int64, error) {
	// Stage 1: validate the UUID before passing it to the database driver.
	if err := domain.ValidateWalletID(walletID); err != nil {
		return 0, err
	}
	// Stage 2: fetch the balance through the persistence boundary.
	return s.store.Balance(ctx, walletID)
}

// TransferHistory validates a wallet identifier and returns its most recent transfers.
func (s *TransferService) TransferHistory(ctx context.Context, walletID string, limit int) ([]domain.Transfer, error) {
	// Stage 1: validate request parameters at the application boundary.
	if err := domain.ValidateWalletID(walletID); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		return nil, domain.ErrInvalidRequest
	}
	// Stage 2: query the repository for the wallet's source and destination transfers.
	return s.store.TransferHistory(ctx, walletID, limit)
}
