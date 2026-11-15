package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Robustrade/wallet-transfer-assignment/internal/config"
	"github.com/Robustrade/wallet-transfer-assignment/internal/database"
	"github.com/Robustrade/wallet-transfer-assignment/internal/middleware"
	"github.com/Robustrade/wallet-transfer-assignment/internal/router"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.Load()
	if err != nil {
		fatal("load config", err)
	}

	if err := database.Migrate(cfg.DB.URL()); err != nil {
		fatal("migrate database", err)
	}
	if err := database.Connect(cfg.DB.URL()); err != nil {
		fatal("connect database", err)
	}

	gin.SetMode(cfg.GinMode)
	r := gin.New()
	if err := r.SetTrustedProxies(nil); err != nil {
		fatal("configure trusted proxies", err)
	}
	r.Use(gin.Recovery(), middleware.RequestLogger())
	router.SetupRoutes(r)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("server started", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal("server stopped unexpectedly", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
	}
	if err := database.Close(); err != nil {
		slog.Error("close database", "error", err)
	}
	slog.Info("server stopped")
}

func fatal(message string, err error) {
	slog.Error(message, "error", err)
	os.Exit(1)
}
