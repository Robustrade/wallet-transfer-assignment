package handler

import (
	"net/http"

	"wallet-transfer/internal/service"
)

type TransferHandler struct {
	transferService service.TransferService
}

func NewTransferHandler(transferService service.TransferService) *TransferHandler {
	return &TransferHandler{transferService: transferService}
}

func (h *TransferHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /transfers", h.CreateTransfer)
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var request CreateTransferRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	transfer, err := h.transferService.CreateTransfer(r.Context(), service.CreateTransferInput{
		IdempotencyKey: request.IdempotencyKey,
		FromWalletID:   request.FromWalletID,
		ToWalletID:     request.ToWalletID,
		Amount:         request.Amount,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, TransferResponse{
		TransferID: transfer.ID,
		Status:     string(transfer.Status),
	})
}
