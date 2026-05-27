package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

const maxTransferRequestBodyBytes = 1 << 20

type TransferService interface {
	CreateTransfer(ctx context.Context, cmd domain.TransferCommand) (domain.Outcome, error)
}

type Handler struct {
	service TransferService
}

func NewHandler(service TransferService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", h.createTransfer)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

func (h *Handler) createTransfer(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxTransferRequestBodyBytes)
	var request createTransferRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeDecodeError(w, err)
		return
	}
	var extra struct{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeDecodeError(w, err)
		return
	}

	outcome, err := h.service.CreateTransfer(r.Context(), domain.TransferCommand{
		IdempotencyKey: request.IdempotencyKey,
		FromWalletID:   request.FromWalletID,
		ToWalletID:     request.ToWalletID,
		Amount:         request.Amount,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidTransfer):
			writeJSON(w, http.StatusBadRequest, domain.APIResponse{Error: err.Error()})
		case errors.Is(err, service.ErrIdempotencyConflict):
			writeJSON(w, http.StatusConflict, domain.APIResponse{Error: err.Error()})
		case errors.Is(err, service.ErrIdempotencyInFlight):
			writeJSON(w, http.StatusConflict, domain.APIResponse{Error: err.Error()})
		default:
			writeJSON(w, http.StatusInternalServerError, domain.APIResponse{Error: "internal server error"})
		}
		return
	}

	writeJSON(w, outcome.StatusCode, outcome.Body)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		writeJSON(w, http.StatusRequestEntityTooLarge, domain.APIResponse{Error: "request body exceeds maximum size"})
		return
	}
	writeJSON(w, http.StatusBadRequest, domain.APIResponse{Error: "invalid JSON request body"})
}
