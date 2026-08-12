package httpapi

import (
	"fmt"
	"strconv"
	"time"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
)

type createTransferRequest struct {
	IdempotencyKey string      `json:"idempotencyKey"`
	FromWalletID   string      `json:"fromWalletId"`
	ToWalletID     string      `json:"toWalletId"`
	Amount         strictInt64 `json:"amount"`
	Currency       string      `json:"currency"`
}

// strictInt64 rejects exactly the shapes docs/api.md promises to reject:
// non-integers ("100.5") and JSON strings ("100"). This can't be done with
// encoding/json.Number — despite being intended for numeric fields, its
// underlying type is string, so the stdlib decoder happily accepts a JSON
// string value into it (json.Number("100") unmarshals from `"100"` just as
// readily as from `100`). A custom UnmarshalJSON that inspects the raw
// token is the only way to actually enforce "must be a JSON number".
type strictInt64 int64

func (a *strictInt64) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		return fmt.Errorf("amount must be a JSON number, not a string")
	}
	n, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return fmt.Errorf("amount must be an integer number of minor units")
	}
	*a = strictInt64(n)
	return nil
}

type transferResponse struct {
	ID             string  `json:"id"`
	IdempotencyKey string  `json:"idempotencyKey"`
	FromWalletID   string  `json:"fromWalletId"`
	ToWalletID     string  `json:"toWalletId"`
	Amount         int64   `json:"amount"`
	Currency       string  `json:"currency"`
	Status         string  `json:"status"`
	FailureCode    *string `json:"failureCode"`
	FailureMessage *string `json:"failureMessage"`
	CreatedAt      string  `json:"createdAt"`
	SettledAt      *string `json:"settledAt"`
}

func newTransferResponse(t domain.Transfer) transferResponse {
	resp := transferResponse{
		ID:             t.ID.String(),
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.AmountMinor,
		Currency:       t.Currency,
		Status:         string(t.Status),
		CreatedAt:      formatTime(t.CreatedAt),
	}
	if t.FailureCode != "" {
		code := string(t.FailureCode)
		resp.FailureCode = &code
	}
	if t.FailureMessage != "" {
		msg := t.FailureMessage
		resp.FailureMessage = &msg
	}
	if t.SettledAt != nil {
		s := formatTime(*t.SettledAt)
		resp.SettledAt = &s
	}
	return resp
}

type createWalletRequest struct {
	ID       string `json:"id"`
	Currency string `json:"currency"`
}

type walletResponse struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Currency  string `json:"currency"`
	Balance   int64  `json:"balance"`
	CreatedAt string `json:"createdAt"`
}

func newWalletResponse(w domain.Wallet) walletResponse {
	return walletResponse{
		ID:        w.ID,
		Kind:      string(w.Kind),
		Currency:  w.Currency,
		Balance:   w.BalanceMinor,
		CreatedAt: formatTime(w.CreatedAt),
	}
}

type ledgerEntryResponse struct {
	ID           int64  `json:"id"`
	TransferID   string `json:"transferId"`
	Type         string `json:"type"`
	Amount       int64  `json:"amount"`
	Currency     string `json:"currency"`
	BalanceAfter int64  `json:"balanceAfter"`
	CreatedAt    string `json:"createdAt"`
}

type ledgerEntriesResponse struct {
	Entries      []ledgerEntryResponse `json:"entries"`
	NextBeforeID *int64                `json:"nextBeforeId"`
}

func newLedgerEntriesResponse(entries []domain.LedgerEntry, nextBeforeID *int64) ledgerEntriesResponse {
	out := make([]ledgerEntryResponse, len(entries))
	for i, e := range entries {
		out[i] = ledgerEntryResponse{
			ID:           e.ID,
			TransferID:   e.TransferID.String(),
			Type:         string(e.EntryType),
			Amount:       e.AmountMinor,
			Currency:     e.Currency,
			BalanceAfter: e.BalanceAfter,
			CreatedAt:    formatTime(e.CreatedAt),
		}
	}
	return ledgerEntriesResponse{Entries: out, NextBeforeID: nextBeforeID}
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
