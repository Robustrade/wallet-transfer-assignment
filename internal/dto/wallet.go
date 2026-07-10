package dto

type TransferRequest struct {
	IdempotencyKey string  `json:"idempotency_key"`
	FromWalletID   string  `json:"from_wallet_id"`
	ToWalletID     string  `json:"to_wallet_id"`
	Amount         float64 `json:"amount"`
}

type TransferResponse struct {
	TransactionID string  `json:"transaction_id"`
	FromWalletID  string  `json:"from_wallet_id"`
	ToWalletID    string  `json:"to_wallet_id"`
	Amount        float64 `json:"amount"`
	Status        string  `json:"status"`
	Timestamp     int64   `json:"timestamp"`
}

type WalletBalanceResponse struct {
	WalletID  string  `json:"wallet_id"`
	OwnerName string  `json:"owner_name"`
	Balance   float64 `json:"balance"`
}

type TransferHistoryEntry struct {
	TransactionID string  `json:"transaction_id"`
	Type          string  `json:"type"`
	FromWalletID  string  `json:"from_wallet_id"`
	ToWalletID    string  `json:"to_wallet_id"`
	Amount        float64 `json:"amount"`
	Status        string  `json:"status"`
	Timestamp     int64   `json:"timestamp"`
}

type TransferHistoryResponse struct {
	WalletID  string                 `json:"wallet_id"`
	Transfers []TransferHistoryEntry `json:"transfers"`
	Limit     int                    `json:"limit"`
	Offset    int                    `json:"offset"`
}
