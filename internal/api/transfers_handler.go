package api

import (
	"encoding/json"
	"net/http"

	"wallet-transfer-assignment/internal/service"
)

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	Currency       string `json:"currency"`
}

func (server *Server) handleCreateTransfer(w http.ResponseWriter, r *http.Request) {
	var request createTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "INVALID_JSON"})
		return
	}

	result, err := server.transfers.CreateTransfer(r.Context(), service.CreateTransferRequest{
		IdempotencyKey: request.IdempotencyKey,
		FromWalletID:   request.FromWalletID,
		ToWalletID:     request.ToWalletID,
		Amount:         request.Amount,
		Currency:       request.Currency,
	})
	if err != nil {
		writeError(w, err)
		return
	}

	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, result.Transfer)
}

func (server *Server) handleGetTransfer(w http.ResponseWriter, r *http.Request) {
	result, err := server.transfers.GetTransfer(r.Context(), pathValue(r, "id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
