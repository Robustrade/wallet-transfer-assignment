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
	// TODO: Decode and validate the HTTP request, call the service,
	// and map the result or error to an HTTP response.
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
