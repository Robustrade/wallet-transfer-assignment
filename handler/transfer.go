package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Robustrade/wallet-transfer-assignment/model"
	"github.com/Robustrade/wallet-transfer-assignment/service"
	"github.com/google/uuid"
)

type createTransferRequest struct {
	IdempotencyKey string          `json:"idempotencyKey"`
	FromWalletID   string          `json:"fromWalletId"`
	ToWalletID     string          `json:"toWalletId"`
	Amount         json.RawMessage `json:"amount"`
}

func (h *Handler) createTransfer(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed json")
		return
	}

	key := strings.TrimSpace(req.IdempotencyKey)
	from := strings.TrimSpace(req.FromWalletID)
	to := strings.TrimSpace(req.ToWalletID)
	if key == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "idempotency key required")
		return
	}
	if from == "" || to == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "wallet id required")
		return
	}
	if from == to {
		writeError(w, http.StatusBadRequest, "invalid_request", "from and to wallets must differ")
		return
	}
	amount, err := parsePositiveIntAmount(req.Amount)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	outcome, err := h.svc.CreateTransfer(r.Context(), key, from, to, amount)
	if err != nil {
		// Same-wallet / amount / empty-id validation already returned 400 above.
		writeServiceError(w, err, func() {
			switch {
			case errors.Is(err, service.ErrWalletNotFound):
				writeError(w, http.StatusNotFound, "wallet_not_found", "")
			case errors.Is(err, service.ErrIdempotencyMismatch):
				writeError(w, http.StatusConflict, "idempotency_key_mismatch", "")
			case errors.Is(err, model.ErrBalanceOverflow):
				writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			case errors.Is(err, service.ErrInvariantBroken):
				writeError(w, http.StatusInternalServerError, "internal", "")
			default:
				writeError(w, http.StatusInternalServerError, "internal", "")
			}
		})
		return
	}

	if outcome.IsReplay() {
		writeRawJSON(w, outcome.StatusCode, outcome.ReplayBody)
		return
	}
	writeJSON(w, outcome.StatusCode, outcome.Transfer)
}

func (h *Handler) getTransfer(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid transfer id")
		return
	}
	view, err := h.svc.GetTransfer(r.Context(), id)
	if err != nil {
		writeServiceError(w, err, func() {
			if errors.Is(err, service.ErrTransferNotFound) {
				writeError(w, http.StatusNotFound, "transfer_not_found", "")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "")
		})
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func parsePositiveIntAmount(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, model.ErrInvalidAmount
	}
	var num json.Number
	if err := json.Unmarshal(raw, &num); err != nil {
		return 0, model.ErrInvalidAmount
	}
	// Reject floats: number must look like an integer.
	if strings.ContainsAny(num.String(), ".eE") {
		return 0, model.ErrInvalidAmount
	}
	v, err := num.Int64()
	if err != nil || v <= 0 {
		return 0, model.ErrInvalidAmount
	}
	return v, nil
}
