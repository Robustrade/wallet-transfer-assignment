package domain

type TransferStatus string

const (
	Pending TransferStatus = "PENDING"
	Success TransferStatus = "SUCCESS"
	Failed  TransferStatus = "FAILED"
)

type Transfer struct {
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotency_key"`
	FromWalletID   string         `json:"from_wallet_id"`
	ToWalletID     string         `json:"to_wallet_id"`
	Amount         int64          `json:"amount"`
	Status         TransferStatus `json:"status"`
}
