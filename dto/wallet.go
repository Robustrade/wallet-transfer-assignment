package dto

import "github.com/Robustrade/wallet-transfer-assignment/model"

// WalletView is the API-facing wallet shape (camelCase JSON).
type WalletView struct {
	ID             string `json:"id"`
	OpeningBalance int64  `json:"openingBalance"`
	Balance        int64  `json:"balance"`
}

// NewWalletView maps a model wallet to the API view.
func NewWalletView(w *model.Wallet) WalletView {
	return WalletView{
		ID:             w.ID,
		OpeningBalance: w.OpeningBalance.Int64(),
		Balance:        w.Balance.Int64(),
	}
}
