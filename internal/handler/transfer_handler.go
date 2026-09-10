package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(result.ResponseCode)
		_, _ = w.Write(result.ResponseBody)
		return
	}

	writeJSON(w, http.StatusCreated, result.Transfer)
}

func (h *TransferHandler) GetTransfer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REQUEST", "transfer id is required")
		return
	}

	transfer, err := h.service.GetTransfer(r.Context(), id)
	if err != nil {
		mapTransferError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, transfer)
}

func (h *TransferHandler) ListWalletTransfers(w http.ResponseWriter, r *http.Request) {
	walletID := chi.URLParam(r, "id")
	if walletID == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REQUEST", "wallet id is required")
		return
	}

	var limit, offset int32
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.ParseInt(limitStr, 10, 32); err == nil {
			limit = int32(l)
		}
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.ParseInt(offsetStr, 10, 32); err == nil {
			offset = int32(o)
		}
	}

	transfers, err := h.service.ListWalletTransfers(r.Context(), walletID, limit, offset)
	if err != nil {
		mapWalletTransferHistoryError(w, err)
		return
	}

	if transfers == nil {
		transfers = []domain.Transfer{}
	}

	writeJSON(w, http.StatusOK, transfers)
}
