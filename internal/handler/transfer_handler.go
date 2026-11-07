// Package handler is the HTTP transport: it decodes requests, calls the service
// and maps results and errors to HTTP responses. It holds no business rules.
package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/celinagupta/wallet-transfer-assignment/internal/domain"
	"github.com/celinagupta/wallet-transfer-assignment/internal/service"
)

// TransferCreator is the service operation the handler depends on.
type TransferCreator interface {
	CreateTransfer(ctx context.Context, req domain.TransferRequest) (service.Result, error)
}

type TransferHandler struct {
	svc    TransferCreator
	logger *slog.Logger
}

func NewTransferHandler(svc TransferCreator, logger *slog.Logger) *TransferHandler {
	return &TransferHandler{svc: svc, logger: logger}
}

// Register adds the transfer routes to mux.
func (h *TransferHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /transfers", h.createTransfer)
}

func (h *TransferHandler) createTransfer(w http.ResponseWriter, r *http.Request) {
	// TODO: implement
}
