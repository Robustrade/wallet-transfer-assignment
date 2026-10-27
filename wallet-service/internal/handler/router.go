// Package handler maps HTTP requests and responses to application operations.
package handler

import "net/http"

// NewRouter returns the application's HTTP routes.
func NewRouter(transfers transferService) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("POST /transfers", createTransfer(transfers))
	return mux
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
}
