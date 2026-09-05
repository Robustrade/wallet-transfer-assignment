package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"wallet-transfer/internal/config"
	apphttp "wallet-transfer/internal/http"
	"wallet-transfer/internal/metrics"
	"wallet-transfer/internal/repository/postgres"
	"wallet-transfer/internal/service"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	})))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := postgres.Open(ctx, cfg.DatabaseURL, postgres.PoolConfig{
		MaxOpenConns:    25,
		MaxIdleConns:    25,
		ConnMaxLifetime: 30 * time.Minute,
	})
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	if _, err := postgres.Migrate(ctx, db, cfg.MigrationsDir); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}

	engine := buildEngine(cfg, db)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      cfg.RequestTimeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		slog.Info("server listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}
	slog.Info("server stopped gracefully")
}

func buildEngine(cfg config.Config, db *sql.DB) *gin.Engine {
	if !cfg.IsDevelopment() {
		gin.SetMode(gin.ReleaseMode)
	}

	txManager := postgres.NewTxManager(db)
	wallets := postgres.NewWalletRepository(txManager)
	transfers := postgres.NewTransferRepository(txManager)
	ledger := postgres.NewLedgerRepository(txManager)
	idempotency := postgres.NewIdempotencyRepository(txManager)

	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return apphttp.NewRouter(apphttp.RouterConfig{
		Transfers:      service.NewTransferService(txManager, wallets, transfers, ledger, idempotency),
		Wallets:        service.NewWalletService(wallets, transfers),
		Health:         postgres.NewHealth(db),
		Metrics:        metrics.New(registry),
		Registry:       registry,
		RequestTimeout: cfg.RequestTimeout,
	})
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
