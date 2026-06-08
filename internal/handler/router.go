package handler

import (
	"net/http"

	"github.com/aks-95/wallet-transfer-assignment/internal/service"
)

type Dependencies struct {
	TransferService *service.TransferService
}

func NewRouter(deps Dependencies) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", Health)

	if deps.TransferService != nil {
		transferHandler := NewTransferHandler(deps.TransferService)
		mux.HandleFunc("POST /transfers", transferHandler.CreateTransfer)
	}

	return mux
}
