package domain

import "errors"

var (
	ErrWalletNotFound        = errors.New("wallet not found")
	ErrWalletExists          = errors.New("wallet already exists")
	ErrInvalidRequest        = errors.New("invalid request")
	ErrIdempotencyConflict   = errors.New("idempotency key was used for a different request")
	ErrIdempotencyInProgress = errors.New("idempotency request is still in progress")
)

type TransferStatus string

type WalletNotFoundError struct {
	WalletID string
}

func (e WalletNotFoundError) Error() string {
	return "wallet not found: " + e.WalletID
}

func (e WalletNotFoundError) Is(target error) bool {
	return target == ErrWalletNotFound
}

func NewWalletNotFoundError(walletID string) error {
	return WalletNotFoundError{WalletID: walletID}
}

const (
	TransferPending   TransferStatus = "PENDING"
	TransferProcessed TransferStatus = "PROCESSED"
	TransferFailed    TransferStatus = "FAILED"
)

type Wallet struct {
	ID      string `json:"walletId"`
	Balance int64  `json:"balance"`
}

type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type Transfer struct {
	ID             string         `json:"transferId"`
	IdempotencyKey string         `json:"idempotencyKey"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         int64          `json:"amount"`
	Status         TransferStatus `json:"status"`
	CreatedAt      string         `json:"createdAt"`
	FailureReason  string         `json:"failureReason,omitempty"`
	LedgerEntries  []LedgerEntry  `json:"ledgerEntries,omitempty"`
}

type TransferResult struct {
	Transfer   Transfer
	HTTPStatus int
}

type LedgerEntry struct {
	ID         int64  `json:"entryId"`
	WalletID   string `json:"walletId"`
	TransferID string `json:"transferId"`
	Type       string `json:"type"`
	Amount     int64  `json:"amount"`
}
