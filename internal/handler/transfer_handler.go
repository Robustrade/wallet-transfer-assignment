package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Saket121/wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	transferService *service.TransferService
}

func NewTransferHandler(ts *service.TransferService) *TransferHandler {
	return &TransferHandler{transferService: ts}
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req service.TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.transferService.CreateTransfer(r.Context(), req)
	if err != nil {
		if strings.Contains(err.Error(), "validation error") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	status := http.StatusCreated
	if result.Idempotent {
		status = http.StatusOK
	}

	writeJSON(w, status, result.Transfer)
}
