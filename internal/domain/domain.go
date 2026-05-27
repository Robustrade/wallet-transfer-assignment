package domain

type TransferState string

const (
	TransferPending   TransferState = "PENDING"
	TransferProcessed TransferState = "PROCESSED"
	TransferFailed    TransferState = "FAILED"
)

type LedgerEntryType string

const (
	LedgerDebit  LedgerEntryType = "DEBIT"
	LedgerCredit LedgerEntryType = "CREDIT"
)

type TransferCommand struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type LedgerEntry struct {
	ID         string          `json:"id"`
	WalletID   string          `json:"walletId"`
	TransferID string          `json:"transferId"`
	Type       LedgerEntryType `json:"type"`
	Amount     int64           `json:"amount"`
}

type Transfer struct {
	ID             string        `json:"id"`
	IdempotencyKey string        `json:"idempotencyKey"`
	FromWalletID   string        `json:"fromWalletId"`
	ToWalletID     string        `json:"toWalletId"`
	Amount         int64         `json:"amount"`
	State          TransferState `json:"state"`
	FailureReason  string        `json:"failureReason,omitempty"`
}

type TransferResponse struct {
	Transfer      Transfer      `json:"transfer"`
	LedgerEntries []LedgerEntry `json:"ledgerEntries,omitempty"`
	FromBalance   *int64        `json:"fromBalance,omitempty"`
	ToBalance     *int64        `json:"toBalance,omitempty"`
}

type APIResponse struct {
	Data  *TransferResponse `json:"data,omitempty"`
	Error string            `json:"error,omitempty"`
}

type Outcome struct {
	StatusCode int
	Body       APIResponse
}
