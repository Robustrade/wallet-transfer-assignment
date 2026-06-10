package handler

import (
	"encoding/json"
	"net/http"
)

// APIResponse is the standard envelope for all API responses.
type APIResponse struct {
	Success bool      `json:"success"`
	Data    any       `json:"data,omitempty"`
	Error   *APIError `json:"error,omitempty"`
}

// APIError represents a structured error in the API response.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeJSON writes a successful JSON response with the given status code and data.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIResponse{ //nolint:errcheck
		Success: true,
		Data:    data,
	})
}

// writeError writes a JSON error response with the given status code, error code, and message.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIResponse{ //nolint:errcheck
		Success: false,
		Error: &APIError{
			Code:    code,
			Message: message,
		},
	})
}
