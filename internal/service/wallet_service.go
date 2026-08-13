package service

import (
	"context"

	"github.com/robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/robustrade/wallet-transfer-assignment/internal/repository"
)

// WalletService is not required by the assignment's core spec, but wallets
// need some way to come into existence and be inspected to make the
// transfer API runnable and testable end to end.
type WalletService struct {
	wallets repository.WalletRepository
	ledger  repository.LedgerRepository
}

func NewWalletService(wallets repository.WalletRepository, ledger repository.LedgerRepository) *WalletService {
	return &WalletService{wallets: wallets, ledger: ledger}
}

func (s *WalletService) CreateWallet(ctx context.Context, in CreateWalletInput) (*WalletResult, error) {
	currency := in.Currency
	if currency == "" {
		currency = "USD"
	}
	w := &domain.Wallet{ID: in.ID, Balance: in.Balance, Currency: currency}
	if err := s.wallets.Create(ctx, w); err != nil {
		return nil, err
	}
	return walletResult(w), nil
}

func (s *WalletService) GetWallet(ctx context.Context, id string) (*WalletResult, error) {
	w, err := s.wallets.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return walletResult(w), nil
}

func (s *WalletService) ListLedger(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
	if _, err := s.wallets.Get(ctx, walletID); err != nil {
		return nil, err
	}
	return s.ledger.ListByWallet(ctx, walletID)
}
