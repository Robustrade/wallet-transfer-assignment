package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"wallet-transfer-assignment/internal/httpapi"
)

func TestLoggingMiddlewareAllowsNilLogger(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	handler := httpapi.LoggingMiddleware(next, nil)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}
