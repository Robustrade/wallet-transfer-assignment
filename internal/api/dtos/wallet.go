package dtos

type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletId   string `json:"fromWalletId"`
	ToWalletId     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type CreateWalletRequest struct {
	WalletId       string `json:"walletId"`
	InitialBalance int64  `json:"initialBalance"`
}

type WalletResponse struct {
	WalletId string `json:"walletId"`
	Balance  int64  `json:"balance"`
}
