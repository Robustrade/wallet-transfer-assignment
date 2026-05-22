package requests

type TransferRequest struct {
	SourceWalletID string `json:"fromWalletId" binding:"required,uuid"`
	DestWalletID   string `json:"toWalletId" binding:"required,uuid"`
	Amount         int64  `json:"amount" binding:"required,min=100,max=1000000"`
}
