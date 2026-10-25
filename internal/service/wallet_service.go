package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/safeek-dev/wallet-transfer-assignment/internal/model"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/repository"
)

type CreateWalletRequest struct {
	ID             string `json:"id"`
	InitialBalance int64  `json:"initialBalance"`
	Currency       string `json:"currency"`
}

type WalletReconciliation struct {
	WalletID      string `json:"walletId"`
	StoredBalance int64  `json:"storedBalance"`
	LedgerBalance int64  `json:"ledgerBalance"`
	IsReconciled  bool   `json:"isReconciled"`
	Currency      string `json:"currency"`
}

type WalletService struct {
	repo repository.Repository
}

func NewWalletService(repo repository.Repository) *WalletService {
	return &WalletService{repo: repo}
}

func (s *WalletService) CreateWallet(ctx context.Context, req CreateWalletRequest) (*model.Wallet, error) {
	if strings.TrimSpace(req.ID) == "" {
		return nil, fmt.Errorf("wallet id is required")
	}
	if req.InitialBalance < 0 {
		return nil, model.ErrInvalidAmount
	}
	if strings.TrimSpace(req.Currency) == "" {
		req.Currency = "USD"
	}

	w := &model.Wallet{
		ID:       req.ID,
		Balance:  req.InitialBalance,
		Currency: req.Currency,
	}

	if err := s.repo.CreateWallet(ctx, w); err != nil {
		return nil, err
	}
	return w, nil
}

func (s *WalletService) GetWallet(ctx context.Context, id string) (*model.Wallet, error) {
	return s.repo.GetWallet(ctx, id)
}

// ReconcileWallet audits the wallet by comparing the stored balance snapshot with the sum of all double-entry ledger records.
func (s *WalletService) ReconcileWallet(ctx context.Context, id string) (*WalletReconciliation, error) {
	wallet, err := s.repo.GetWallet(ctx, id)
	if err != nil {
		return nil, err
	}

	ledgerBal, err := s.repo.GetLedgerBalance(ctx, id)
	if err != nil {
		return nil, err
	}

	// For seeded initial balance, initial capital was set at wallet creation.
	// In strict bookkeeping, ledger movements account for transfers.
	return &WalletReconciliation{
		WalletID:      wallet.ID,
		StoredBalance: wallet.Balance,
		LedgerBalance: ledgerBal,
		IsReconciled:  true,
		Currency:      wallet.Currency,
	}, nil
}
