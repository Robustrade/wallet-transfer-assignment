package models

import (
	"time"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"
)

// CreateWalletRequest is the wire format for POST /wallets.
type CreateWalletRequest struct {
	ID             string `json:"id"`
	InitialBalance int64  `json:"initialBalance"`
}

// WalletResponse is the wire format for a wallet.
type WalletResponse struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ToWalletResponse builds a WalletResponse from a domain wallet.
func ToWalletResponse(w domain.Wallet) WalletResponse {
	return WalletResponse{ID: w.ID, Balance: w.Balance, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt}
}
