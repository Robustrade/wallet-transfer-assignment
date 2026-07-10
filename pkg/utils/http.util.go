package utils

import (
	"encoding/json"
	"errors"
	"net/http"
)

func WriteSuccess(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

func WriteError(w http.ResponseWriter, err error) {
	statusCode := http.StatusInternalServerError
	message := "something went wrong"

	var customErr *CustomError
	if errors.As(err, &customErr) {
		statusCode = customErr.StatusCode
		message = customErr.Message
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func Wrap(handler http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}
