// Command server wires the layers together and serves the HTTP API.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/example/wallet-transfer/internal/handler"
	"github.com/example/wallet-transfer/internal/migrate"
	"github.com/example/wallet-transfer/internal/repository"
	"github.com/example/wallet-transfer/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dsn := getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable")
	addr := getenv("ADDR", ":8080")

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return err
	}
	if err := migrate.Apply(ctx, pool); err != nil {
		return err
	}

	repo := repository.New()
	svc := service.NewTransferService(pool, repo)
	h := handler.NewTransferHandler(svc)

	mux := http.NewServeMux()
	h.Routes(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
