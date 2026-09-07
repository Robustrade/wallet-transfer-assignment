package handler

import (
	"encoding/json"
	"net/http"

	"Wallet_Transfer_Service/internal/domain"
	"Wallet_Transfer_Service/internal/service"
)

type TransferHandler struct {
	transfers *service.TransferService
}

func NewTransferHandler(transfers *service.TransferService) *TransferHandler {
	return &TransferHandler{transfers: transfers}
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// CreateTransfer handles POST /transfers.
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, domain.ErrInvalidRequest)
		return
	}

	result, replayed, err := h.transfers.CreateTransfer(r.Context(), service.CreateTransferRequest{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})
	if err != nil {
		writeError(w, err)
		return
	}

	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

// GetTransfer handles GET /transfers/{id}, returning the transfer plus
// the ledger entries it produced (two entries for a PROCESSED transfer,
// none for a FAILED one).
func (h *TransferHandler) GetTransfer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	detail, err := h.transfers.GetTransfer(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, transferDetailResponse(detail))
}

type ledgerEntryResponse struct {
	EntryID    int64            `json:"entryId"`
	WalletID   string           `json:"walletId"`
	TransferID string           `json:"transferId"`
	Type       domain.EntryType `json:"type"`
	Amount     int64            `json:"amount"`
}

type transferResponse struct {
	TransferID     string                `json:"transferId"`
	IdempotencyKey string                `json:"idempotencyKey"`
	FromWalletID   string                `json:"fromWalletId"`
	ToWalletID     string                `json:"toWalletId"`
	Amount         int64                 `json:"amount"`
	State          domain.TransferState  `json:"state"`
	FailureReason  string                `json:"failureReason,omitempty"`
	LedgerEntries  []ledgerEntryResponse `json:"ledgerEntries"`
}

func transferDetailResponse(d *service.TransferDetail) transferResponse {
	entries := make([]ledgerEntryResponse, len(d.Entries))
	for i, e := range d.Entries {
		entries[i] = ledgerEntryResponse{
			EntryID:    e.ID,
			WalletID:   e.WalletID,
			TransferID: e.TransferID,
			Type:       e.EntryType,
			Amount:     e.Amount,
		}
	}
	return transferResponse{
		TransferID:     d.Transfer.ID,
		IdempotencyKey: d.Transfer.IdempotencyKey,
		FromWalletID:   d.Transfer.FromWalletID,
		ToWalletID:     d.Transfer.ToWalletID,
		Amount:         d.Transfer.Amount,
		State:          d.Transfer.State,
		FailureReason:  d.Transfer.FailureReason,
		LedgerEntries:  entries,
	}
}
