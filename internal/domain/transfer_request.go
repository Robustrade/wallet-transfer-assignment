package domain

type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletId   string `json:"fromWalletId"`
	ToWalletId     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}
