package domain

type CreateTransferResponse struct {
	TransferId   string `json:"transferId"`
	Status       string `json:"status"`
	FromWalletId string `json:"fromWalletId"`
	ToWalletId   string `json:"toWalletId"`
	Amount       int64  `json:"amount"`
}
