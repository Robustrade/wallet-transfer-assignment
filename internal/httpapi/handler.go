package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/mshabab1296/wallet-transfer-assignment/internal/domain"
)

type TransferCreator interface {
	CreateTransfer(context.Context, domain.CreateTransferRequest) (domain.Transfer, error)
}
type Handler struct{ transfers TransferCreator }

func New(transfers TransferCreator) *Handler { return &Handler{transfers: transfers} }
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", h.createTransfer)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}
func (h *Handler) createTransfer(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req domain.CreateTransferRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON request body"})
		return
	}
	transfer, err := h.transfers.CreateTransfer(r.Context(), req)
	if err == nil {
		writeJSON(w, http.StatusCreated, transfer)
		return
	}
	switch {
	case errors.Is(err, domain.ErrInvalidRequest):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, domain.ErrIdempotencyConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, domain.ErrWalletNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, domain.ErrInsufficientFunds):
		writeJSON(w, http.StatusUnprocessableEntity, transfer)
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
