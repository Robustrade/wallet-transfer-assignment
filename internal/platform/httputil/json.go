package httputil

import (
	"encoding/json"
	"net/http"
)

func JSONContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		next.ServeHTTP(w, r)
	})
}

func WriteJSON(w http.ResponseWriter, status int, v any) error {
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

func WriteJSONBytes(w http.ResponseWriter, status int, b []byte) error {
	w.WriteHeader(status)
	_, err := w.Write(b)
	return err
}
