package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/vardaanmittal09/wallet-transfer-assignment/internal/domain"
	"github.com/vardaanmittal09/wallet-transfer-assignment/internal/service"
)

type transferServiceAdapter struct {
	service *service.TransferService
}

func RegisterRoutes(mux *http.ServeMux, svc *service.TransferService) {
	adapter := transferServiceAdapter{service: svc}
	mux.HandleFunc("POST /transfers", adapter.createTransfer)
	mux.HandleFunc("GET /transfers/", adapter.getTransfer)
	mux.HandleFunc("GET /wallets/", adapter.getWallet)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

func (a transferServiceAdapter) createTransfer(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var input service.CreateTransferInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}

	result, err := a.service.CreateTransfer(r.Context(), input)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	status := http.StatusCreated
	if result.Replay {
		status = http.StatusOK
	}
	if result.State == domain.TransferFailed {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, result)
}

func (a transferServiceAdapter) getTransfer(w http.ResponseWriter, r *http.Request) {
	transferID := strings.TrimPrefix(r.URL.Path, "/transfers/")
	if transferID == "" {
		writeError(w, http.StatusNotFound, "transfer not found")
		return
	}
	result, err := a.service.GetTransfer(r.Context(), transferID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a transferServiceAdapter) getWallet(w http.ResponseWriter, r *http.Request) {
	walletID := strings.TrimPrefix(r.URL.Path, "/wallets/")
	if walletID == "" {
		writeError(w, http.StatusNotFound, "wallet not found")
		return
	}
	wallet, err := a.service.GetWallet(r.Context(), walletID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, wallet)
}

func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrAmountMustBePositive),
		errors.Is(err, domain.ErrIdempotencyKeyRequired),
		errors.Is(err, domain.ErrWalletIDRequired),
		errors.Is(err, domain.ErrSameWalletTransfer):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrWalletNotFound), errors.Is(err, domain.ErrTransferNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
