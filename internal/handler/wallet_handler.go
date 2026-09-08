package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

type WalletHandler struct {
	service *service.WalletService
}

func NewWalletHandler(svc *service.WalletService) *WalletHandler {
	return &WalletHandler{service: svc}
}

type createWalletRequest struct {
	ID      string `json:"id"`
	Balance int64  `json:"balance"`
}

func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var req createWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid json body")
		return
	}

	wallet, err := h.service.CreateWallet(r.Context(), req.ID, req.Balance)
	if err != nil {
		if errors.Is(err, service.ErrInvalidWalletID) || errors.Is(err, service.ErrNegativeWalletBalance) {
			writeJSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create wallet")
		return
	}

	writeJSON(w, http.StatusCreated, wallet)
}

func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REQUEST", "wallet id is required")
		return
	}

	wallet, err := h.service.GetWallet(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "wallet not found")
			return
		}
		if errors.Is(err, service.ErrInvalidWalletID) {
			writeJSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to retrieve wallet")
		return
	}

	writeJSON(w, http.StatusOK, wallet)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   code,
		"message": message,
	})
}
