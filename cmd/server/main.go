package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wallet-transfer-assignment/internal/config"
	"wallet-transfer-assignment/internal/database"
	httpHandler "wallet-transfer-assignment/internal/handler/http"
	"wallet-transfer-assignment/internal/repository/postgres"
	"wallet-transfer-assignment/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	startupCtx, cancelStartup := context.WithTimeout(ctx, cfg.StartupTimeout)
	db, err := database.Open(startupCtx, cfg)
	cancelStartup()
	if err != nil {
		// Driver errors may contain connection details; do not log credentials.
		return errors.New("PostgreSQL startup connection failed")
	}
	defer db.Close()
	if ctx.Err() != nil {
		return nil
	}

	// Keep in-flight requests alive during graceful draining, then cancel them
	// on timeout. The signal context controls lifecycle, not request execution.
	requestCtx, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	server := &http.Server{
		Addr: cfg.HTTPAddr, Handler: withRequestTimeout(httpHandler.New(service.New(postgres.New(db), logger)), cfg.HTTPRequestTimeout),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
		BaseContext: func(net.Listener) context.Context { return requestCtx },
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	logger.Info("HTTP server starting", "address", cfg.HTTPAddr)
	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
	}
	logger.Info("HTTP server shutting down")
	// The signal context is canceled already; use a fresh bounded context.
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		cancelRequests()
		server.Close()
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	logger.Info("HTTP server stopped")
	return nil
}

// WriteTimeout bounds network writes; this deadline also bounds service/DB work.
// The handler remains synchronous so Shutdown continues to track active work.
func withRequestTimeout(next http.Handler, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})

}
