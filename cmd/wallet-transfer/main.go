package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"wallet-transfer-assignment/internal/httpapi"
	"wallet-transfer-assignment/internal/service"
	"wallet-transfer-assignment/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	maxOpenConns, err := getenvInt("WALLET_DB_MAX_OPEN_CONNS", 4)
	if err != nil {
		return err
	}
	dsn := getenv("WALLET_DB_DSN", "file:wallets.db?_foreign_keys=on&_busy_timeout=5000")
	repository, err := store.OpenWithOptions(ctx, dsn, store.Options{
		MaxOpenConns: maxOpenConns,
		MaxIdleConns: maxOpenConns,
		EnableWAL:    true,
	})
	if err != nil {
		return err
	}
	defer repository.Close()

	transferService := service.New(repository)
	server := &http.Server{
		Addr:              getenv("ADDR", ":8080"),
		Handler:           httpapi.NewHandler(transferService).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		slog.Info("wallet transfer service listening", "addr", server.Addr)
		errs <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func getenv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getenvInt(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}
