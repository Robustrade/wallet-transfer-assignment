package http

import "net/http"

// NewRouter wires HTTP routes using Go 1.22+ stdlib pattern routing.
func NewRouter(transfers *TransferHandler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", transfers.Create)
	mux.HandleFunc("GET /healthz", health)
	return Logging(mux)
}

// health reports that the process is up and serving.
func health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
