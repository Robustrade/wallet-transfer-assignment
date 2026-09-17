package handler

import (
	"log"
	"net/http"
	"time"
)

// NewRouter sets up all routes and returns an http.Handler with logging middleware.
func NewRouter(
	transferHandler *TransferHandler,
	walletHandler *WalletHandler,
	healthHandler *HealthHandler,
) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/transfers", transferHandler.CreateTransfer)
	mux.HandleFunc("/wallets/", walletHandler.GetWallet)
	mux.HandleFunc("/health", healthHandler.Health)

	return loggingMiddleware(mux)
}

// loggingMiddleware logs each request with method, path, status, and duration.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		log.Printf("%s %s %d %s",
			r.Method,
			r.URL.Path,
			rw.statusCode,
			time.Since(start),
		)
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
