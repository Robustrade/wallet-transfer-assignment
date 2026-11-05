package handler

import (
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// NewRouter wires services into HTTP routes and adds request logging.
// A nil logger discards logs.
func NewRouter(conn *sql.DB, log *slog.Logger) http.Handler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	transfers := &TransferHandler{svc: service.NewTransferService(conn, log), log: log}
	wallets := &WalletHandler{svc: service.NewWalletService(conn), log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", transfers.Create)
	mux.HandleFunc("GET /transfers/{id}", transfers.Get)
	mux.HandleFunc("POST /wallets", wallets.Create)
	mux.HandleFunc("GET /wallets/{id}", wallets.Get)
	mux.HandleFunc("GET /wallets/{id}/ledger", wallets.Ledger)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if err := conn.PingContext(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return logRequests(log, mux)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// logRequests writes one structured log line per request.
func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.InfoContext(r.Context(), "request",
			"method", r.Method, "path", r.URL.Path, "status", rec.status,
			"durationMs", time.Since(start).Milliseconds())
	})
}
