package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/codejk-crypto/wallet-transfer-assignment/internal/handler"
	"github.com/codejk-crypto/wallet-transfer-assignment/internal/migrate"
	"github.com/codejk-crypto/wallet-transfer-assignment/internal/service"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx := context.Background()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Error("connect postgres", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Error("ping postgres", "err", err)
		os.Exit(1)
	}
	if err := migrate.Up(ctx, pool); err != nil {
		log.Error("migrate", "err", err)
		os.Exit(1)
	}

	svc := service.New(pool, log)
	server := &http.Server{
		Addr:              addr,
		Handler:           handler.New(svc).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("listening", "addr", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}
