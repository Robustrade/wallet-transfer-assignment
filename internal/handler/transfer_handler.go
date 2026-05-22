package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/HarshilJariwala2000/wallet-transfer-assignment/internal/domain"
	"github.com/HarshilJariwala2000/wallet-transfer-assignment/internal/service"
)

// TransferHandler is the HTTP transport for transfers. It only validates and
// maps requests/responses; all business logic lives in the service.
type TransferHandler struct {
	svc *service.TransferService
}

func NewTransferHandler(svc *service.TransferService) *TransferHandler {
	return &TransferHandler{svc: svc}
}

// Register wires the handler routes onto a mux.
func (h *TransferHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /transfers", h.create)
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

func (h *TransferHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.IdempotencyKey == "" || req.FromWalletID == "" || req.ToWalletID == "" {
		writeError(w, http.StatusBadRequest, "idempotencyKey, fromWalletId and toWalletId are required")
		return
	}

	result, err := h.svc.CreateTransfer(r.Context(), service.CreateTransferCommand{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})
	if err != nil {
		writeError(w, statusForError(err), err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, result.Response)
}

// statusForError maps domain errors to HTTP status codes.
func statusForError(err error) int {
	switch {
	case errors.Is(err, domain.ErrWalletNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrInvalidAmount), errors.Is(err, domain.ErrSameWallet):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrIdempotencyConflict):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
