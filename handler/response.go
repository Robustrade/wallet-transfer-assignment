package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRawJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
	if len(body) > 0 && body[len(body)-1] != '\n' {
		_, _ = w.Write([]byte("\n"))
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	body := errorBody{Error: code}
	if message != "" {
		body.Message = message
	}
	writeJSON(w, status, body)
}

// writeServiceError maps known transport-level failures, then falls back to fn.
// Context deadline/cancel become 504 so a saturated pool does not surface as 500.
func writeServiceError(w http.ResponseWriter, err error, fn func()) {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		writeError(w, http.StatusGatewayTimeout, "timeout", "request timed out")
		return
	}
	fn()
}
