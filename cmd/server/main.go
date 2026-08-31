package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/mshabab1296/wallet-transfer-assignment/internal/httpapi"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/metrics"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/repository"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/service"
)

var serveHTTP = http.ListenAndServe

func main() {
	if err := run(context.Background(), databaseURL()); err != nil {
		log.Print(err)
	}
}

// databaseURL returns the configured database URL or the local Compose default.
func databaseURL() string {
	// Stage 1: prefer an explicit deployment database URL.
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://wallet:wallet@localhost:5433/wallets?sslmode=disable"
	}
	return dsn
}

// run wires the production dependencies and starts the HTTP server.
func run(ctx context.Context, dsn string) error {
	// Stage 1: open the database and apply pending schema migrations.
	repo, err := repository.OpenPostgres(ctx, dsn)
	if err != nil {
		return err
	}
	defer repo.Close()

	// Stage 2: compose the service, transport, metrics, and structured logger.
	transferService := service.NewTransferService(repo)
	handler := httpapi.New(transferService, transferService, &metrics.Collector{}, slog.Default())

	// Stage 3: start the public HTTP listener.
	log.Printf("wallet transfer service listening on :8080")
	return serveHTTP(":8080", handler.Routes())
}
