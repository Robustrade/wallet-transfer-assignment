package transfer

import (
	"context"
	"errors"
	"strings"
)

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

func (s *Service) CreateWallet(ctx context.Context, wallet Wallet) error {
	return s.store.CreateWallet(ctx, wallet.ID, wallet.BalanceCents)
}

func (s *Service) Transfer(ctx context.Context, request TransferRequest) (Transfer, bool, error) {
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.FromWalletID = strings.TrimSpace(request.FromWalletID)
	request.ToWalletID = strings.TrimSpace(request.ToWalletID)
	if request.IdempotencyKey == "" || request.FromWalletID == "" || request.ToWalletID == "" || request.AmountCents <= 0 {
		return Transfer{}, false, errors.New("idempotencyKey, wallet ids, and a positive amount are required")
	}
	if request.FromWalletID == request.ToWalletID {
		return Transfer{}, false, errors.New("source and destination wallets must differ")
	}
	return s.store.Transfer(ctx, request)
}

func (s *Service) Wallet(ctx context.Context, id string) (Wallet, error) {
	return s.store.GetWallet(ctx, id)
}

func (s *Service) Wallets(ctx context.Context) ([]Wallet, error) {
	return s.store.GetAllWallets(ctx)
}

func (s *Service) Ledger(ctx context.Context, transferID string) ([]LedgerEntry, error) {
	return s.store.LedgerEntries(ctx, transferID)
}

func (s *Service) AllLedgers(ctx context.Context) ([]LedgerEntry, error) {
	return s.store.GetAllLedgerEntries(ctx)
}
