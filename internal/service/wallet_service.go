package service

import (
	"context"
	"strings"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

const (
	defaultLedgerLimit = 50
	maxLedgerLimit     = 200
)

type WalletService struct {
	store repository.Store
}

func NewWalletService(store repository.Store) *WalletService {
	return &WalletService{store: store}
}

type WalletResult struct {
	WalletID string `json:"walletId"`
	OwnerID  string `json:"ownerId"`
	Currency string `json:"currency"`
	Balance  int64  `json:"balance"`
	Status   string `json:"status"`
}

type LedgerEntryResult struct {
	EntryID      int64  `json:"entryId"`
	TransferID   string `json:"transferId"`
	Type         string `json:"type"`
	Amount       int64  `json:"amount"`
	BalanceAfter *int64 `json:"balanceAfter"`
	CreatedAt    string `json:"createdAt"`
}

func (service *WalletService) GetWallet(ctx context.Context, id string) (WalletResult, error) {
	wallet, err := service.store.GetWallet(ctx, strings.TrimSpace(id))
	if err != nil {
		return WalletResult{}, err
	}
	return buildWalletResult(wallet), nil
}

func (service *WalletService) ListLedger(ctx context.Context, walletID string, limit int, cursor int64) ([]LedgerEntryResult, error) {
	walletID = strings.TrimSpace(walletID)
	if walletID == "" {
		return nil, domain.ErrInvalidWalletID
	}
	if limit <= 0 {
		limit = defaultLedgerLimit
	}
	if limit > maxLedgerLimit {
		limit = maxLedgerLimit
	}

	entries, err := service.store.ListWalletLedger(ctx, walletID, limit, cursor)
	if err != nil {
		return nil, err
	}
	results := make([]LedgerEntryResult, 0, len(entries))
	for _, entry := range entries {
		results = append(results, LedgerEntryResult{
			EntryID:      entry.ID,
			TransferID:   entry.TransferID,
			Type:         string(entry.Type),
			Amount:       entry.Amount,
			BalanceAfter: entry.BalanceAfter,
			CreatedAt:    formatTime(entry.CreatedAt),
		})
	}
	return results, nil
}

func buildWalletResult(wallet domain.Wallet) WalletResult {
	return WalletResult{
		WalletID: wallet.ID,
		OwnerID:  wallet.OwnerID,
		Currency: wallet.Currency,
		Balance:  wallet.Balance,
		Status:   string(wallet.Status),
	}
}
