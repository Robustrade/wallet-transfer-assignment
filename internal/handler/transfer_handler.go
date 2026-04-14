package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	svc service.TransferService
}

func NewTransferHandler(svc service.TransferService) *TransferHandler {
	return &TransferHandler{svc: svc}
}

// CreateTransfer handles POST /transfers
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req service.TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	transfer, err := h.svc.Transfer(r.Context(), req)
	if err != nil {
		if errors.Is(err, service.ErrInvalidRequest) || errors.Is(err, service.ErrInsufficientFunds) {
			h.respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if errors.Is(err, service.ErrWalletNotFound) {
			h.respondJSON(w, http.StatusNotFound, map[string]string{"error": "wallet not found"})
			return
		}
		// Generic internal error
		h.respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	if transfer.IsIdempotentReplay {
		h.respondJSON(w, http.StatusOK, transfer)
	} else {
		h.respondJSON(w, http.StatusCreated, transfer)
	}
}

func (h *TransferHandler) respondJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}
