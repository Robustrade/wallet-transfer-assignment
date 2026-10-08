package http

import "net/http"

func New(service TransferService) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/transfers", transfers(service))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeError(w, 404, "NOT_FOUND", "Route not found") })
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Use GET for health")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{\"status\":\"ok\"}\n"))
	})
	return mux
}
