package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// GetTransfer returns a transfer by id, or domain.ErrTransferNotFound.
func (s *TransferService) GetTransfer(ctx context.Context, id uuid.UUID) (domain.Transfer, error) {
	return s.db.Transfers().Get(ctx, id)
}

// WalletService serves wallet reads. It depends only on WalletReader, so it can
// never lock or move money.
type WalletService struct {
	wallets WalletReader
}

// NewWalletService returns a WalletService reading from wallets.
func NewWalletService(wallets WalletReader) *WalletService {
	return &WalletService{wallets: wallets}
}

// GetWallet returns a wallet with its current balance, or domain.ErrWalletNotFound.
func (s *WalletService) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	return s.wallets.Get(ctx, id)
}
