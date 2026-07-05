package service

import (
	"context"
	"fmt"

	"github.com/karthikadarvin/wallet-transfer-assignment/internal/repository"
)

type WalletBalanceResult struct {
	WalletID string `json:"walletId"`
	Balance  int64  `json:"balance"`
}

type TransferHistoryItem struct {
	TransferID    string `json:"transferId"`
	FromWalletID  string `json:"fromWalletId"`
	ToWalletID    string `json:"toWalletId"`
	Amount        int64  `json:"amount"`
	Status        string `json:"status"`
	FailureReason string `json:"failureReason,omitempty"`
	CreatedAt     string `json:"createdAt"`
}

type WalletQueryService struct {
	db        Querier
	wallets   repository.WalletRepository
	transfers repository.TransferRepository
}

// Querier here is satisfied by *sql.DB; kept as an interface so this
// service can also be tested against a real DB without a transaction.
type Querier interface {
	repository.Querier
}

func NewWalletQueryService(db Querier, wallets repository.WalletRepository, transfers repository.TransferRepository) *WalletQueryService {
	return &WalletQueryService{db: db, wallets: wallets, transfers: transfers}
}

func (s *WalletQueryService) GetBalance(ctx context.Context, walletID string) (*WalletBalanceResult, error) {
	w, err := s.wallets.Get(ctx, s.db, walletID)
	if err != nil {
		return nil, err
	}
	return &WalletBalanceResult{WalletID: w.ID, Balance: w.Balance}, nil
}

func (s *WalletQueryService) GetHistory(ctx context.Context, walletID string, limit, offset int) ([]TransferHistoryItem, error) {
	// Confirm the wallet exists so a bad ID returns 404, not an empty list.
	if _, err := s.wallets.Get(ctx, s.db, walletID); err != nil {
		return nil, err
	}

	transfers, err := s.transfers.ListByWallet(ctx, s.db, walletID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list transfers: %w", err)
	}

	items := make([]TransferHistoryItem, 0, len(transfers))
	for _, t := range transfers {
		items = append(items, TransferHistoryItem{
			TransferID:    t.ID,
			FromWalletID:  t.FromWalletID,
			ToWalletID:    t.ToWalletID,
			Amount:        t.Amount,
			Status:        string(t.Status),
			FailureReason: t.FailureReason,
			CreatedAt:     t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	return items, nil
}
