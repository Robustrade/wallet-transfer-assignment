package handler

import "net/http"

// NewRouter registers HTTP routes for the API.
func NewRouter(transfers *TransferHandler, health *HealthHandler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", methodHandler(http.MethodGet, health.Health))
	mux.HandleFunc("/transfers", methodHandler(http.MethodPost, transfers.CreateTransfer))
	return mux
}

func methodHandler(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}
