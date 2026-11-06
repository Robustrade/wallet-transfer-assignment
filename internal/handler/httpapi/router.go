// Package httpapi is the HTTP transport: it decodes and shape-checks requests,
// invokes the service layer and maps results and errors to HTTP responses.
// It contains no business rules.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// TransferCreator executes transfers (POST /transfers).
type TransferCreator interface {
	CreateTransfer(ctx context.Context, req domain.TransferRequest) (service.CreateTransferResult, error)
}

// TransferReader looks up transfers (GET /transfers/{id}).
type TransferReader interface {
	GetTransfer(ctx context.Context, id uuid.UUID) (domain.Transfer, error)
}

// TransferService is everything the HTTP layer needs for transfers; each handler
// depends only on the half it uses.
type TransferService interface {
	TransferCreator
	TransferReader
}

// WalletReader looks up wallets.
type WalletReader interface {
	GetWallet(ctx context.Context, id string) (domain.Wallet, error)
}

// Pinger reports whether a dependency (the database) is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps are the collaborators the HTTP layer needs.
type Deps struct {
	Transfers TransferService
	Wallets   WalletReader
	Readiness Pinger // optional; nil means always ready
	Logger    *slog.Logger
}

// NewRouter returns the HTTP handler for the service, wrapped in middleware.
func NewRouter(d Deps) http.Handler {
	transfers := &transferHandler{svc: d.Transfers, logger: d.Logger}
	queries := &queryHandler{transfers: d.Transfers, wallets: d.Wallets, logger: d.Logger}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", transfers.create)
	mux.HandleFunc("GET /transfers/{id}", queries.getTransfer)
	mux.HandleFunc("GET /wallets/{id}", queries.getWallet)
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /readyz", readyz(d.Readiness, d.Logger))

	return withRequestID(withAccessLog(d.Logger, withRecovery(d.Logger, mux)))
}
