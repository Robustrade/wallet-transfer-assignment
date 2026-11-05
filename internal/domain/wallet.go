package domain

type WalletStatus string

const (
	WalletStatusActive   WalletStatus = "ACTIVE"
	WalletStatusInactive WalletStatus = "INACTIVE"
)

type Wallet struct {
	ID        string       `json:"walletId"`
	Balance   int64        `json:"balance"`
	Status    WalletStatus `json:"status"`
	CreatedAt string       `json:"createdAt"`
	UpdatedAt string       `json:"updatedAt"`
}
