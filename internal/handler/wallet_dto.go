package handler

type CreateWalletRequest struct {
	WalletID       string `json:"walletId"`
	InitialBalance int64  `json:"initialBalance"`
	Status         string `json:"status,omitempty"`
}

type AddFundsRequest struct {
	Amount int64 `json:"amount"`
}

type WalletResponse struct {
	WalletID  string `json:"walletId"`
	Balance   int64  `json:"balance"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type LedgerEntryResponse struct {
	ID         string `json:"id"`
	WalletID   string `json:"walletId"`
	TransferID string `json:"transferId,omitempty"`
	Type       string `json:"type"`
	Amount     int64  `json:"amount"`
}
