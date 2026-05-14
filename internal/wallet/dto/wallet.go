package dto

// GetWalletResponse represents the API response for getting wallet balance
type GetWalletResponse struct {
	WalletID  string `json:"walletId"`
	Balance   int64  `json:"balance"`
	Currency  string `json:"currency"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// WalletTransfersResponse represents a list of transfers for a wallet
type WalletTransfersResponse struct {
	Transfers []TransferResponse `json:"transfers"`
	Total     int                `json:"total"`
}

type TransferResponse struct {
	TransferID  string `json:"transferId"`
	OtherWallet string `json:"otherWallet"`
	Amount      int64  `json:"amount"`
	Type        string `json:"type"` // "INCOMING" or "OUTGOING"
	Status      string `json:"status"`
	CreatedAt   string `json:"createdAt"`
}
