package transfer

import (
	"crypto/rand"
	"encoding/hex"
)

type Wallet struct {
	ID           string `json:"id"`
	BalanceCents int64  `json:"balanceCents"`
}

type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	AmountCents    int64  `json:"amount"`
}

type Transfer struct {
	ID             string `json:"id"`
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	AmountCents    int64  `json:"amount"`
	Status         string `json:"status"`
}

type LedgerEntry struct {
	TransferID  string `json:"transferId"`
	WalletID    string `json:"walletId"`
	EntryType   string `json:"type"`
	AmountCents int64  `json:"amount"`
}

func newID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}
