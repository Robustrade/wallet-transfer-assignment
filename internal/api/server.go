package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"wallet-transfer-assignment/internal/service"
)

type transferService interface {
	CreateTransfer(ctx context.Context, request service.CreateTransferRequest) (service.CreateTransferResult, error)
	GetTransfer(ctx context.Context, id string) (service.TransferResult, error)
}

type walletService interface {
	GetWallet(ctx context.Context, id string) (service.WalletResult, error)
	ListLedger(ctx context.Context, walletID string, limit int, cursor int64) ([]service.LedgerEntryResult, error)
}

type Server struct {
	transfers transferService
	wallets   walletService
	logger    *slog.Logger
}

func NewServer(transfers transferService, wallets walletService, logger *slog.Logger) *Server {
	return &Server{
		transfers: transfers,
		wallets:   wallets,
		logger:    logger,
	}
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.handleHealth)
	mux.HandleFunc("POST /transfers", server.handleCreateTransfer)
	mux.HandleFunc("GET /transfers/{id}", server.handleGetTransfer)
	mux.HandleFunc("GET /wallets/{id}", server.handleGetWallet)
	mux.HandleFunc("GET /wallets/{id}/ledger", server.handleGetWalletLedger)
	return server.requestLogger(mux)
}

func (server *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		server.logger.Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
	})
}

func pathValue(r *http.Request, name string) string {
	return strings.TrimSpace(r.PathValue(name))
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(status int) {
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}
