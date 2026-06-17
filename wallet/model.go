package wallet

import "time"

type Wallet struct {
	ID        uint64    `json:"id"`
	WalletID  string    `json:"walletId"`
	Balance   float64   `json:"balance"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
