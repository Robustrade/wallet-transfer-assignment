package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wallet-transfer-golang/internal/wallet"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(os.Getenv("LOG_LEVEL")),
	}))
	slog.SetDefault(logger)

	databasePath := envOrDefault("WALLET_DB_PATH", "wallets.sqlite3")
	host := envOrDefault("HOST", "127.0.0.1")
	port := envOrDefault("PORT", "8000")
	repository, err := wallet.OpenRepository(databasePath)
	if err != nil {
		logger.Error("initialize wallet database", "error", err)
		os.Exit(1)
	}
	defer repository.Close()

	handler := wallet.NewHTTPHandler(wallet.NewService(repository), logger)
	server := wallet.NewHTTPServer(net.JoinHostPort(host, port), handler)
	shutdownContext, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	go func() {
		<-shutdownContext.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("graceful server shutdown failed", "error", err)
		}
	}()

	logger.Info("wallet transfer service listening", "address", server.Addr, "database", databasePath)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("HTTP server stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func parseLogLevel(value string) slog.Level {
	switch value {
	case "DEBUG", "debug":
		return slog.LevelDebug
	case "WARN", "warn", "WARNING", "warning":
		return slog.LevelWarn
	case "ERROR", "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
