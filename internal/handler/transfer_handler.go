package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/priiyank-xd/wallet-transfer-service/internal/domain"
	"github.com/priiyank-xd/wallet-transfer-service/internal/service"
)

type TransferHandler struct {
	svc *service.TransferService
}

func NewTransferHandler(svc *service.TransferService) *TransferHandler {
	return &TransferHandler{svc: svc}
}

func (h *TransferHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /transfers", h.CreateTransfer)
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {

	var req domain.TransferRequest
	var resp domain.TransferResponse

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, domain.TransferResponse{Error: "Invalid request body"})
		return
	}

	resp, status, err := h.svc.CreateTransfer(r.Context(), req)
	if err != nil {
		resp.Error = err.Error()
	}

	writeJSON(w, mapHTTPStatus(status, err), resp)

}

func mapHTTPStatus(status int, err error) int {
	if status != 0 {
		return status
	}
	if err == nil {
		return http.StatusOK
	}

	switch {
	case errors.Is(err, domain.ErrInsufficientFunds),
		errors.Is(err, domain.ErrIdempotencyConflict):
		return http.StatusConflict
	case errors.Is(err, domain.ErrEmptyIdempotency),
		errors.Is(err, domain.ErrEmptyWalletID),
		errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrInvalidRequest):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrWalletNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
