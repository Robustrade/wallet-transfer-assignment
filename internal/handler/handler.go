package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"
)

// Service is the interface the handler layer depends on.
type Service interface {
	CreateWallet(ctx context.Context, req service.CreateWalletRequest) (*domain.Wallet, error)
	GetWallet(ctx context.Context, id string) (*domain.Wallet, error)
	CreateTransfer(ctx context.Context, req service.CreateTransferRequest) (*domain.Transfer, error)
	GetTransferHistory(ctx context.Context, walletID string) ([]domain.Transfer, error)
}

// Handler holds the HTTP handler methods for the wallet transfer service.
type Handler struct {
	svc Service
}

// New creates a Handler wired to the given service.
func New(svc Service) *Handler {
	return &Handler{svc: svc}
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to write response", "error", err)
	}
}

func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
