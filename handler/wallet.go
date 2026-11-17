package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Robustrade/wallet-transfer-assignment/model"
	"github.com/Robustrade/wallet-transfer-assignment/service"
)

type createWalletRequest struct {
	ID      string `json:"id"`
	Balance *int64 `json:"balance"`
}

func (h *Handler) createWallet(w http.ResponseWriter, r *http.Request) {
	var req createWalletRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed json")
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "wallet id required")
		return
	}
	if req.Balance == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "balance required")
		return
	}
	if *req.Balance < 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "balance must be non-negative")
		return
	}

	view, err := h.svc.CreateWallet(r.Context(), req.ID, *req.Balance)
	if err != nil {
		writeServiceError(w, err, func() {
			switch {
			case errors.Is(err, service.ErrWalletAlreadyExists):
				writeError(w, http.StatusConflict, "wallet_already_exists", "wallet id already exists, choose a different one")
			case errors.Is(err, model.ErrOpeningBalanceImmutable):
				writeError(w, http.StatusConflict, "opening_balance_immutable", "opening_balance is immutable")
			case errors.Is(err, model.ErrInvalidWalletID), errors.Is(err, model.ErrNegativeBalance):
				writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			default:
				writeError(w, http.StatusInternalServerError, "internal", "")
			}
		})
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (h *Handler) getWallet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "wallet id required")
		return
	}
	view, err := h.svc.GetWallet(r.Context(), id)
	if err != nil {
		writeServiceError(w, err, func() {
			if errors.Is(err, service.ErrWalletNotFound) {
				writeError(w, http.StatusNotFound, "wallet_not_found", "")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "")
		})
		return
	}
	writeJSON(w, http.StatusOK, view)
}
