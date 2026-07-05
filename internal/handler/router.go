package handler

import "net/http"

// NewRouter registers all HTTP routes and returns the top-level handler.
func NewRouter(transferHandler *TransferHandler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthCheck)
	mux.HandleFunc("POST /transfers", transferHandler.CreateTransfer)
	return mux
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
