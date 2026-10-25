package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/dale-tomson/wallet-transfer-assignment/internal/domain"
	"github.com/dale-tomson/wallet-transfer-assignment/internal/service"
)

type WalletHandler struct {
	svc service.WalletService
}

func NewWalletHandler(svc service.WalletService) *WalletHandler {
	return &WalletHandler{svc: svc}
}

type CreateWalletRequest struct {
	UserID   string `json:"userId"`
	Currency string `json:"currency"`
}

func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var req CreateWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.Currency) == "" {
		writeError(w, http.StatusBadRequest, "userId and currency are required")
		return
	}

	wallet, err := h.svc.CreateWallet(r.Context(), req.UserID, strings.ToUpper(req.Currency))
	if err != nil {
		h.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, wallet)
}

func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	walletID := r.PathValue("id")
	if walletID == "" {
		writeError(w, http.StatusBadRequest, "wallet id is required")
		return
	}

	wallet, err := h.svc.GetWallet(r.Context(), walletID)
	if err != nil {
		h.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, wallet)
}

func (h *WalletHandler) Transfer(w http.ResponseWriter, r *http.Request) {
	var req service.TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json payload")
		return
	}

	// Sourced primarily from body as per ASSIGNMENT.md; header acts as clean fallback
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}

	if req.IdempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "idempotencyKey is required")
		return
	}

	resp, err := h.svc.Transfer(r.Context(), req)
	if err != nil {
		h.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *WalletHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrWalletNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrInsufficientFunds):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, domain.ErrConcurrentRequest):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrIdempotencyKeyConflict):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, domain.ErrCurrencyMismatch),
		errors.Is(err, domain.ErrSelfTransfer),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrIdempotencyKeyRequired):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
