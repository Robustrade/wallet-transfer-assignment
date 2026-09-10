package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// TransferResponse represents the API response for a transfer.
type TransferResponse struct {
	ID             string              `json:"id"`
	IdempotencyKey string              `json:"idempotencyKey"`
	FromWalletID   string              `json:"fromWalletId"`
	ToWalletID     string              `json:"toWalletId"`
	Amount         int64               `json:"amount"`
	Status         string              `json:"status"`
	FailureReason  string              `json:"failureReason,omitempty"`
	LedgerEntries  []LedgerEntryDTO    `json:"ledgerEntries,omitempty"`
	CreatedAt      time.Time           `json:"createdAt"`
	UpdatedAt      time.Time           `json:"updatedAt"`
}

// LedgerEntryDTO represents a ledger entry in the API response.
type LedgerEntryDTO struct {
	ID         string    `json:"id"`
	TransferID string    `json:"transferId"`
	WalletID   string    `json:"walletId"`
	EntryType  string    `json:"entryType"`
	Amount     int64     `json:"amount"`
	CreatedAt  time.Time `json:"createdAt"`
}

// WalletResponse represents the API response for a wallet.
type WalletResponse struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TransactionHistoryResponse represents the API response for transaction history.
type TransactionHistoryResponse struct {
	Entries []LedgerEntryDTO `json:"entries"`
	Count   int              `json:"count"`
}

// ErrorResponse represents an API error response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// toTransferResponse converts a service result to an API response.
func toTransferResponse(result *service.TransferResult) *TransferResponse {
	resp := &TransferResponse{
		ID:             result.Transfer.ID,
		IdempotencyKey: result.Transfer.IdempotencyKey,
		FromWalletID:   result.Transfer.FromWalletID,
		ToWalletID:     result.Transfer.ToWalletID,
		Amount:         result.Transfer.Amount,
		Status:         string(result.Transfer.Status),
		FailureReason:  result.Transfer.FailureReason,
		CreatedAt:      result.Transfer.CreatedAt,
		UpdatedAt:      result.Transfer.UpdatedAt,
	}

	if result.LedgerEntries != nil {
		resp.LedgerEntries = toLedgerEntryDTOs(result.LedgerEntries)
	}

	return resp
}

// toWalletResponse converts a domain wallet to an API response.
func toWalletResponse(wallet *domain.Wallet) *WalletResponse {
	return &WalletResponse{
		ID:        wallet.ID,
		Balance:   wallet.Balance,
		Currency:  wallet.Currency,
		CreatedAt: wallet.CreatedAt,
		UpdatedAt: wallet.UpdatedAt,
	}
}

// toTransactionHistoryResponse converts ledger entries to an API response.
func toTransactionHistoryResponse(entries []*domain.LedgerEntry) *TransactionHistoryResponse {
	dtos := toLedgerEntryDTOs(entries)
	return &TransactionHistoryResponse{
		Entries: dtos,
		Count:   len(dtos),
	}
}

// toLedgerEntryDTOs converts domain ledger entries to DTOs.
func toLedgerEntryDTOs(entries []*domain.LedgerEntry) []LedgerEntryDTO {
	dtos := make([]LedgerEntryDTO, 0, len(entries))
	for _, e := range entries {
		dtos = append(dtos, LedgerEntryDTO{
			ID:         e.ID,
			TransferID: e.TransferID,
			WalletID:   e.WalletID,
			EntryType:  string(e.EntryType),
			Amount:     e.Amount,
			CreatedAt:  e.CreatedAt,
		})
	}
	return dtos
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{
		Error:   http.StatusText(status),
		Message: message,
	})
}
