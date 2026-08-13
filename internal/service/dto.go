package service

import (
	"time"

	"github.com/robustrade/wallet-transfer-assignment/internal/domain"
)

type CreateTransferInput struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// TransferResult is the transport-agnostic view of a transfer returned by
// the service layer; handlers map it to their wire format.
type TransferResult struct {
	TransferID     string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         domain.TransferStatus
	FailureReason  string
	CreatedAt      time.Time
	// Replayed is true when this result was reconstructed from a prior
	// completed transfer for the same idempotency key, rather than being
	// freshly processed on this call.
	Replayed bool
}

func transferResult(t *domain.Transfer, replayed bool) *TransferResult {
	return &TransferResult{
		TransferID:     t.ID,
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount,
		Status:         t.Status,
		FailureReason:  t.FailureReason,
		CreatedAt:      t.CreatedAt,
		Replayed:       replayed,
	}
}

type CreateWalletInput struct {
	ID       string
	Balance  int64
	Currency string
}

type WalletResult struct {
	ID        string
	Balance   int64
	Currency  string
	CreatedAt time.Time
}

func walletResult(w *domain.Wallet) *WalletResult {
	return &WalletResult{ID: w.ID, Balance: w.Balance, Currency: w.Currency, CreatedAt: w.CreatedAt}
}
