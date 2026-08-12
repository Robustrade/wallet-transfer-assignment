package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Swapnil811/wallet-transfer-assignment/wallet-transfer-service/internal/domain"
	"github.com/Swapnil811/wallet-transfer-assignment/wallet-transfer-service/internal/service"
	"github.com/google/uuid"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(s *service.TransferService) *TransferHandler {
	return &TransferHandler{service: s}
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type transferResponse struct {
	TransferID   string `json:"transferId"`
	Status       string `json:"status"`
	FromWalletID string `json:"fromWalletId"`
	ToWalletID   string `json:"toWalletId"`
	Amount       int64  `json:"amount"`
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req createTransferRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if strings.TrimSpace(req.IdempotencyKey) == "" ||
		strings.TrimSpace(req.FromWalletID) == "" ||
		strings.TrimSpace(req.ToWalletID) == "" || req.Amount <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "idempotencyKey, fromWalletId, toWalletId and positive amount are required",
		})
		return
	}

	fromID, err := uuid.Parse(req.FromWalletID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid fromWalletId"})
		return
	}
	toID, err := uuid.Parse(req.ToWalletID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid toWalletId"})
		return
	}

	result, err := h.service.CreateTransfer(r.Context(), domain.CreateTransferRequest{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         req.Amount,
	})
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrWalletNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		case errors.Is(err, domain.ErrIdempotencyConflict):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		case errors.Is(err, domain.ErrInvalidTransfer):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
		return
	}

	response := transferResponse{
		TransferID:   result.Transfer.ID.String(),
		Status:       string(result.Transfer.Status),
		FromWalletID: result.Transfer.FromWalletID.String(),
		ToWalletID:   result.Transfer.ToWalletID.String(),
		Amount:       result.Transfer.Amount,
	}

	status := http.StatusCreated
	if result.Transfer.Status == domain.TransferFailed {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, response)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
