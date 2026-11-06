// Package app wires the layers together: repository → service → HTTP handler.
package app

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Robustrade/wallet-transfer-assignment/internal/handler/httpapi"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// NewHandler builds the fully wired HTTP handler on top of pool.
func NewHandler(pool *pgxpool.Pool, lockTimeout time.Duration, logger *slog.Logger) http.Handler {
	db := postgres.New(pool, postgres.Options{LockTimeout: lockTimeout})
	// The workflow is decorated with logging; metrics or tracing would wrap it the same way.
	transfers := service.NewLoggingTransfers(service.NewTransferService(db), logger)

	return httpapi.NewRouter(httpapi.Deps{
		Transfers: transfers,
		Wallets:   service.NewWalletService(db.Wallets()),
		Readiness: pool,
		Logger:    logger,
	})
}
