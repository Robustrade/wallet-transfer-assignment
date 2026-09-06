// Package http contains thin HTTP handlers: request validation, transport
// (de)serialization, and status-code mapping. No business logic lives here.
package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	transfers *service.TransferService
}

func NewTransferHandler(transfers *service.TransferService) *TransferHandler {
	return &TransferHandler{transfers: transfers}
}

func (h *TransferHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	result, err := h.transfers.Execute(r.Context(), service.TransferRequest{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, toTransferResponse(result.Transfer))
}

func (h *TransferHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	t, entries, err := h.transfers.GetTransfer(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	resp := transferDetailResponse{transferResponse: toTransferResponse(t)}
	for _, e := range entries {
		resp.LedgerEntries = append(resp.LedgerEntries, ledgerEntryResponse{
			ID:        e.ID,
			WalletID:  e.WalletID,
			Type:      string(e.Type),
			Amount:    e.Amount,
			CreatedAt: e.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrIdempotencyKeyRequired),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSameWallet):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, domain.ErrWalletNotFound):
		writeError(w, http.StatusNotFound, "wallet_not_found", err.Error())
	case errors.Is(err, domain.ErrTransferNotFound):
		writeError(w, http.StatusNotFound, "transfer_not_found", err.Error())
	case errors.Is(err, domain.ErrIdempotencyKeyConflict):
		writeError(w, http.StatusConflict, "idempotency_key_conflict", err.Error())
	case errors.Is(err, service.ErrWalletAlreadyExists):
		writeError(w, http.StatusConflict, "wallet_already_exists", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "an unexpected error occurred")
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: code, Message: message})
}
