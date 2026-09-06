package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jaiminbhaduri/wallet-transfer-assignment/internal/domain"
)

// transferService is the subset of *service.TransferService the handler
// depends on, kept as an interface so handler tests can supply a fake.
type transferService interface {
	CreateTransfer(ctx context.Context, req domain.CreateTransferRequest) (*domain.Transfer, error)
	GetWalletBalance(ctx context.Context, walletID string) (*domain.Wallet, error)
}

type TransferHandler struct {
	svc transferService
}

func NewTransferHandler(svc transferService) *TransferHandler {
	return &TransferHandler{svc: svc}
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var body createTransferRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	req := domain.CreateTransferRequest{
		IdempotencyKey: body.IdempotencyKey,
		FromWalletID:   body.FromWalletID,
		ToWalletID:     body.ToWalletID,
		Amount:         body.Amount,
	}

	transfer, err := h.svc.CreateTransfer(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrEmptyIdempotencyKey),
			errors.Is(err, domain.ErrEmptyWalletID),
			errors.Is(err, domain.ErrSameWallet),
			errors.Is(err, domain.ErrInvalidAmount):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, domain.ErrWalletNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	writeJSON(w, http.StatusCreated, toTransferResponseDTO(transfer))
}

func (h *TransferHandler) GetWalletBalance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	wallet, err := h.svc.GetWalletBalance(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toWalletResponseDTO(wallet))
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponseDTO{Error: msg})
}
