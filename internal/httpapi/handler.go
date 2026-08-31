package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/domain"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/metrics"
)

// WalletReader exposes wallet queries needed by the HTTP transport.
type WalletReader interface {
	Balance(context.Context, string) (int64, error)
	TransferHistory(context.Context, string, int) ([]domain.Transfer, error)
}

// TransferCreator is the service contract used by the HTTP transport.
type TransferCreator interface {
	CreateTransfer(context.Context, domain.CreateTransferRequest) (domain.Transfer, error)
}

// Handler maps HTTP requests and responses without containing transfer business rules.
type Handler struct {
	transfers TransferCreator
	wallets   WalletReader
	metrics   *metrics.Collector
	logger    *slog.Logger
}

// New creates an HTTP handler backed by wallet and transfer service contracts.
func New(transfers TransferCreator, wallets WalletReader, collector *metrics.Collector, logger *slog.Logger) *Handler {
	if collector == nil {
		collector = &metrics.Collector{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{transfers: transfers, wallets: wallets, metrics: collector, logger: logger}
}

// Routes returns the service's public HTTP routes with request logging and metrics.
func (h *Handler) Routes() http.Handler {
	// Stage 1: register the application endpoints.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", h.createTransfer)
	mux.HandleFunc("GET /wallets/{id}/balance", h.walletBalance)
	mux.HandleFunc("GET /wallets/{id}/transfers", h.transferHistory)
	mux.Handle("GET /metrics", h.metrics)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Stage 2: wrap every route in structured logging and request metrics.
	return h.observe(mux)
}

func (h *Handler) createTransfer(w http.ResponseWriter, r *http.Request) {
	// Stage 1: decode and validate the bounded JSON request body.
	defer r.Body.Close()
	var req domain.CreateTransferRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.respondTransfer(w, http.StatusBadRequest, nil, map[string]string{"error": "invalid JSON request body"})
		return
	}

	// Stage 2: execute the idempotent transfer workflow and map domain errors.
	transfer, err := h.transfers.CreateTransfer(r.Context(), req)
	if err == nil {
		h.respondTransfer(w, http.StatusCreated, &transfer, transfer)
		return
	}
	switch {
	case errors.Is(err, domain.ErrInvalidRequest):
		h.respondTransfer(w, http.StatusBadRequest, nil, map[string]string{"error": err.Error()})
	case errors.Is(err, domain.ErrIdempotencyConflict):
		h.respondTransfer(w, http.StatusConflict, nil, map[string]string{"error": err.Error()})
	case errors.Is(err, domain.ErrWalletNotFound):
		h.respondTransfer(w, http.StatusNotFound, nil, map[string]string{"error": err.Error()})
	case errors.Is(err, domain.ErrInsufficientFunds):
		h.respondTransfer(w, http.StatusUnprocessableEntity, &transfer, transfer)
	default:
		h.respondTransfer(w, http.StatusInternalServerError, nil, map[string]string{"error": "internal server error"})
	}
}

func (h *Handler) walletBalance(w http.ResponseWriter, r *http.Request) {
	// Stage 1: retrieve the requested wallet balance through the service layer.
	walletID := r.PathValue("id")
	balance, err := h.wallets.Balance(r.Context(), walletID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	// Stage 2: return the balance in the transport contract.
	writeJSON(w, http.StatusOK, map[string]any{"walletId": walletID, "balance": balance})
}

func (h *Handler) transferHistory(w http.ResponseWriter, r *http.Request) {
	// Stage 1: parse the optional, bounded history page size.
	limit, err := historyLimit(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": domain.ErrInvalidRequest.Error()})
		return
	}
	// Stage 2: load the wallet's recent source and destination transfers.
	transfers, err := h.wallets.TransferHistory(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transfers": transfers})
}

func (h *Handler) respondTransfer(w http.ResponseWriter, status int, transfer *domain.Transfer, body any) {
	writeJSON(w, status, body)
	if h.metrics != nil {
		replayed := transfer != nil && transfer.Replayed
		h.metrics.RecordTransfer(status, replayed)
	}
}

func (h *Handler) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Stage 1: establish a request ID and capture the final response status.
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", requestID)
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		started := time.Now()

		// Stage 2: serve the route, publish metrics, and emit a structured completion log.
		next.ServeHTTP(recorder, r)
		if h.metrics != nil {
			h.metrics.RecordHTTP()
		}
		h.logger.Info("http request completed", "request_id", requestID, "method", r.Method, "route", r.Pattern, "status", recorder.status, "duration_ms", time.Since(started).Milliseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func historyLimit(r *http.Request) (int, error) {
	if value := r.URL.Query().Get("limit"); value != "" {
		return strconv.Atoi(value)
	}
	return 50, nil
}

func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidRequest):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, domain.ErrWalletNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
