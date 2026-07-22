// Package handler wires HTTP routing and owns the server lifecycle.
package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.uber.org/zap"
	"wallet-transfer/internal/config"
	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/observability/metrics"
)

// TransferService is the handler-facing service seam (satisfied by
// service.TransferService). Defining it here keeps handlers unit-testable.
type TransferService interface {
	CreateTransfer(ctx context.Context, p domain.NewTransferParams) (domain.Transfer, bool, error)
	GetTransfer(ctx context.Context, id uuid.UUID) (domain.Transfer, error)
}

// Server holds HTTP dependencies and the underlying http.Server.
type Server struct {
	cfg       config.Config
	log       *zap.Logger
	pool      *pgxpool.Pool
	transfers TransferService
	metrics   *metrics.Metrics
	http      *http.Server
}

// New constructs the server, builds the mux router, and configures timeouts.
func New(cfg config.Config, log *zap.Logger, pool *pgxpool.Pool, transfers TransferService, m *metrics.Metrics) *Server {
	s := &Server{cfg: cfg, log: log, pool: pool, transfers: transfers, metrics: m}
	s.http = &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           otelhttp.NewHandler(s.router(), "http.server"),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return s
}

// router builds the mux router with cross-cutting middleware, operational
// endpoints at the root, and business endpoints under /api/v1.
func (s *Server) router() *mux.Router {
	r := mux.NewRouter()
	r.Use(requestIDMiddleware)
	r.Use(recoverMiddleware(s.log))
	r.Use(accessLogMiddleware(s.log))
	r.Use(metricsMiddleware(s.metrics))

	r.NotFoundHandler = http.HandlerFunc(s.handleNotFound)
	r.MethodNotAllowedHandler = http.HandlerFunc(s.handleMethodNotAllowed)

	r.HandleFunc("/healthz", s.handleHealthz).Methods(http.MethodGet)
	r.HandleFunc("/readyz", s.handleReadyz).Methods(http.MethodGet)
	r.Handle("/metrics", s.metrics.Handler()).Methods(http.MethodGet)

	// Transfer endpoints are served both at the assignment paths (/transfers) and
	// under a versioned prefix (/api/v1/transfers).
	registerTransfers := func(router *mux.Router) {
		router.HandleFunc("/transfers", s.handleCreateTransfer).Methods(http.MethodPost)
		router.HandleFunc("/transfers/{id}", s.handleGetTransfer).Methods(http.MethodGet)
	}
	registerTransfers(r)
	registerTransfers(r.PathPrefix("/api/v1").Subrouter())
	return r
}

// Handler exposes the router for testing and composition.
func (s *Server) Handler() http.Handler { return s.http.Handler }

// Run serves until ctx is cancelled, then lets ongoing requests finish within the
// configured shutdown timeout.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("http server listening", zap.String("addr", s.cfg.HTTPAddr))
		errCh <- s.http.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		s.log.Info("shutdown signal received, draining connections")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer cancel()
		return s.http.Shutdown(shutdownCtx)
	}
}
