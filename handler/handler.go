package handler

import (
	"context"
	"net/http"

	"github.com/Robustrade/wallet-transfer-assignment/dto"
	"github.com/google/uuid"
)

// Service is the business API the HTTP layer depends on.
// *service.Service implements it; tests can inject a fake.
type Service interface {
	CreateWallet(ctx context.Context, id string, balance int64) (dto.WalletView, error)
	GetWallet(ctx context.Context, id string) (dto.WalletView, error)
	CreateTransfer(ctx context.Context, idempotencyKey, fromWalletID, toWalletID string, amount int64) (dto.TransferOutcome, error)
	GetTransfer(ctx context.Context, id uuid.UUID) (dto.TransferView, error)
}

// Handler is the HTTP transport adapter. It validates requests and maps service results.
type Handler struct {
	svc Service
}

func New(svc Service) *Handler {
	return &Handler{svc: svc}
}

// Routes registers API endpoints on mux.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("POST /wallets", h.createWallet)
	mux.HandleFunc("GET /wallets/{id}", h.getWallet)
	mux.HandleFunc("POST /transfers", h.createTransfer)
	mux.HandleFunc("GET /transfers/{id}", h.getTransfer)
}
