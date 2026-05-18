package app

type TransferResponse struct {
	TransferID     string `json:"transferId"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	State          string `json:"state"`
	IdempotencyKey string `json:"idempotencyKey"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

type WalletResponse struct {
	ID      string `json:"id"`
	Balance int64  `json:"balance"`
}
