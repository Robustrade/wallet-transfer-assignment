package handler

import (
	"net/http"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(svc *service.TransferService) *TransferHandler {
	return &TransferHandler{service: svc}
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateTransferRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	result, err := h.service.ExecuteTransfer(r.Context(), req)
	if err != nil {
		mapTransferError(w, err)
		return
	}

	if result.IsReplay {
		w.Header().Set("X-Idempotent-Replay", "true")
		writeJSON(w, http.StatusOK, result.Transfer)
		return
	}

	writeJSON(w, http.StatusCreated, result.Transfer)
}
