// Command server is the HTTP entry point for the wallet-transfer service.
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

	"github.com/gorilla/mux"

	"github.com/Robustrade/wallet-transfer-assignment/common/health"
	config "github.com/Robustrade/wallet-transfer-assignment/config/init"
	transferinit "github.com/Robustrade/wallet-transfer-assignment/services/transfer/init"
)

func main() {
	if err := run(); err != nil {
		// Log to stderr and exit non-zero. We use os package directly here
		// because the logger may not have initialized.
		_, _ = os.Stderr.WriteString("server: " + err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	rootCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.LoadConfig(rootCtx)
	if err != nil {
		return err
	}
	defer func() { _ = cfg.Close() }()

	// Make the configured logger the process default so any code that uses
	// slog.Default() (notably middleware.WriteError on 5xx) writes through
	// the same JSON handler.
	slog.SetDefault(cfg.Logger)

	router := mux.NewRouter()
	if err := transferinit.InitTransferService(router, transferinit.Config{
		DB:     cfg.DB,
		Logger: cfg.Logger,
	}); err != nil {
		return err
	}

	router.HandleFunc("/healthz", health.Handler(cfg.DB)).Methods(http.MethodGet)

	srv := &http.Server{
		Addr:              ":" + cfg.Common.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	serverErr := make(chan error, 1)
	go func() {
		cfg.Logger.InfoContext(rootCtx, "starting server", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case <-rootCtx.Done():
		cfg.Logger.InfoContext(rootCtx, "shutdown signal received")
	case err, open := <-serverErr:
		if open && err != nil {
			return err
		}
		// server returned ErrServerClosed normally
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Common.ShutdownTimeout)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		cfg.Logger.ErrorContext(shutdownCtx, "graceful shutdown failed", "error", err)
		return err
	}
	cfg.Logger.InfoContext(shutdownCtx, "server stopped cleanly")
	return nil
}
