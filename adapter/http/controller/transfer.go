package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/chaitanya-bhagat/wallet-transfer-assignment/internal/model"
	"github.com/chaitanya-bhagat/wallet-transfer-assignment/internal/services"
)

type TransferController struct {
	service services.Service
}

func NewTransferController(serv services.Service) *TransferController {
	return &TransferController{
		service: serv,
	}
}

func (tc *TransferController) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var input model.CreateTransferRequest
	fmt.Println("in")
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	transfer, err := tc.service.CreateTransfer(r.Context(), input)
	if err != nil {
		fmt.Println("err:", err)
		tc.handleError(w, err)
		return
	}
	fmt.Println(transfer)
	writeJSON(w, http.StatusCreated, transfer)
}

func (tc *TransferController) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, model.ErrInvalidIdempotencyKey):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "idempotency key is required"})

	case errors.Is(err, model.ErrInvalidSourceWallet):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source wallet is required"})
	case errors.Is(err, model.ErrInvalidDestinationWallet):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "destination wallet is required"})
	case errors.Is(err, model.ErrSameWallet):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source and destination wallet must be different"})
	case errors.Is(err, model.ErrInvalidAmount):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "amount must be greater than zero"})
	case errors.Is(err, model.ErrInsufficientFunds):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "insufficient funds"})
	case errors.Is(err, model.ErrWalletNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "wallet not found"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("internal server error: %s", err.Error())})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
