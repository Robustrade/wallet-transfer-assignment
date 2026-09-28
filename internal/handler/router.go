package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/middleware"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

// TransferCreator is the part of service.TransferService the handler uses.
// The interface is declared here, by its consumer, so tests can pass a fake.
type TransferCreator interface {
	CreateTransfer(ctx context.Context, cmd service.CreateTransferCommand) (service.TransferResult, error)
}

// WalletReader is the part of service.WalletService the handler uses.
type WalletReader interface {
	GetWallet(ctx context.Context, id string) (*domain.Wallet, error)
	ListTransfers(ctx context.Context, walletID string, limit int) ([]domain.Transfer, error)
}

// Pinger checks database connectivity (*sql.DB satisfies it).
type Pinger interface {
	PingContext(ctx context.Context) error
}

// RouterConfig holds the router's dependencies.
type RouterConfig struct {
	Transfers      TransferCreator
	Wallets        WalletReader
	DB             Pinger
	Logger         *slog.Logger
	RequestTimeout time.Duration
	MaxBodyBytes   int64
}

// NewRouter builds the HTTP handler.
//
// Middleware order matters:
//  1. RequestID first, so every later step (including error bodies) has the id.
//  2. Logger wraps everything below, so it records the final status even for
//     panics and timeouts.
//  3. Recovery is inside Logger, so a panic becomes a logged 500.
//  4. Timeout, then BodyLimit, apply to the handlers.
func NewRouter(cfg RouterConfig) http.Handler {
	r := gin.New()
	r.Use(
		middleware.RequestID(),
		middleware.Logger(cfg.Logger),
		middleware.Recovery(cfg.Logger),
		middleware.Timeout(cfg.RequestTimeout),
		middleware.BodyLimit(cfg.MaxBodyBytes),
	)

	transfers := &transferHandler{svc: cfg.Transfers}
	wallets := &walletHandler{svc: cfg.Wallets}
	health := &healthHandler{db: cfg.DB}

	r.POST("/transfers", transfers.create)
	r.GET("/wallets/:id", wallets.get)
	r.GET("/wallets/:id/transfers", wallets.listTransfers)
	r.GET("/healthz", health.healthz)
	r.GET("/readyz", health.readyz)

	r.NoRoute(func(c *gin.Context) {
		middleware.AbortWithError(c, http.StatusNotFound, middleware.CodeNotFound, "route not found")
	})
	return r
}
