package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rohitkumar27/wallet-transfer-assignment/internal/domain"
	"github.com/rohitkumar27/wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	svc *service.TransferService
}

func NewTransferHandler(svc *service.TransferService) *TransferHandler {
	return &TransferHandler{svc: svc}
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req domain.TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	result, err := h.svc.CreateTransfer(r.Context(), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, result.StatusCode, result.Body)
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrMissingField),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSameWallet):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrProcessingTimeout):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	body, _ := json.Marshal(service.ErrorResponse{Error: message})
	writeJSON(w, statusCode, body)
}

func writeJSON(w http.ResponseWriter, statusCode int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}
