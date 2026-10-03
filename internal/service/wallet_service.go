package service

import (
	"context"
	"fmt"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CreateWalletRequest represents parameters to initialize a wallet.
type CreateWalletRequest struct {
	ID             string `json:"id,omitempty"`
	InitialBalance int64  `json:"initialBalance"`
	Currency       string `json:"currency,omitempty"`
}

// WalletBalanceResponse represents the wallet balance and audit verification.
type WalletBalanceResponse struct {
	WalletID          string `json:"walletId"`
	StoredBalance     int64  `json:"storedBalance"`
	LedgerBalance     int64  `json:"ledgerBalance"`
	IsAuditConsistent bool   `json:"isAuditConsistent"`
	Currency          string `json:"currency"`
}

// WalletService defines operations for wallet management and balance auditing.
type WalletService interface {
	CreateWallet(ctx context.Context, req CreateWalletRequest) (*domain.Wallet, error)
	GetWallet(ctx context.Context, id string) (*domain.Wallet, error)
	GetBalance(ctx context.Context, id string) (*WalletBalanceResponse, error)
}

type walletService struct {
	repo repository.Repository
}

// NewWalletService constructs a WalletService using the repository.
func NewWalletService(repo repository.Repository) WalletService {
	return &walletService{repo: repo}
}

func (s *walletService) CreateWallet(ctx context.Context, req CreateWalletRequest) (*domain.Wallet, error) {
	if req.InitialBalance < 0 {
		return nil, fmt.Errorf("initial balance cannot be negative")
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}
	walletID := req.ID
	if walletID == "" {
		walletID = "w_" + uuid.New().String()
	}

	now := time.Now().UTC()
	wallet := &domain.Wallet{
		ID:             walletID,
		Balance:        req.InitialBalance,
		OpeningBalance: req.InitialBalance,
		Currency:       req.Currency,
		Status:         domain.WalletStatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	err := s.repo.ExecuteInTransaction(ctx, func(tx *gorm.DB) error {
		return s.repo.Wallets().Create(ctx, tx, wallet)
	})
	if err != nil {
		return nil, err
	}

	return wallet, nil
}

func (s *walletService) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	return s.repo.Wallets().GetByID(ctx, id)
}

func (s *walletService) GetBalance(ctx context.Context, id string) (*WalletBalanceResponse, error) {
	wallet, err := s.repo.Wallets().GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	netTransferDelta, err := s.repo.Ledgers().GetCalculatedBalance(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate ledger balance: %w", err)
	}

	reconciledBal := wallet.OpeningBalance + netTransferDelta

	return &WalletBalanceResponse{
		WalletID:          wallet.ID,
		StoredBalance:     wallet.Balance,
		LedgerBalance:     reconciledBal,
		IsAuditConsistent: wallet.Balance == reconciledBal,
		Currency:          wallet.Currency,
	}, nil
}
