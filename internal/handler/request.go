package handler

type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	FromWalletID   string `json:"from_wallet_id"`
	ToWalletID     string `json:"to_wallet_id"`
	Amount         int64  `json:"amount"`
}
