package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// TransferHandler defines the HTTP handler for wallet transfer operations.
type TransferHandler struct {
	service service.TransferService
}

// NewTransferHandler creates a new HTTP handler with a transfer service dependency.
func NewTransferHandler(service service.TransferService) *TransferHandler {
	return &TransferHandler{service: service}
}

// Router returns the HTTP router for the service endpoints.
func (h *TransferHandler) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/transfers", h.handleCreateTransfer)
	return mux
}

// handleCreateTransfer accepts POST /transfers requests and returns transfer results.
func (h *TransferHandler) handleCreateTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeErrorStatus(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var request TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.writeErrorStatus(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	response, err := h.service.CreateTransfer(r.Context(), service.CreateTransferRequest{
		IdempotencyKey: request.IdempotencyKey,
		FromWalletID:   request.FromWalletID,
		ToWalletID:     request.ToWalletID,
		Amount:         request.Amount,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}

	h.writeJSON(w, http.StatusCreated, response)
}

// writeError maps service errors to HTTP status codes and JSON response bodies.
func (h *TransferHandler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidTransferRequest):
		h.writeErrorStatus(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrWalletNotFound):
		h.writeErrorStatus(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrInsufficientFunds):
		h.writeErrorStatus(w, http.StatusUnprocessableEntity, err.Error())
	default:
		h.writeErrorStatus(w, http.StatusInternalServerError, "internal server error")
	}
}

func (h *TransferHandler) writeErrorStatus(w http.ResponseWriter, status int, message string) {
	h.writeJSON(w, status, map[string]string{"error": message})
}

func (h *TransferHandler) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// TransferRequest is the JSON payload accepted by POST /transfers.
type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}
