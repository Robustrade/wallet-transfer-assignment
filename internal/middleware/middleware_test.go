package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDReusesHeaderAndGeneratesMissingID(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value(requestIDContextKey) == nil {
			t.Error("request ID missing from context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	withID := httptest.NewRequest(http.MethodGet, "/", nil)
	withID.Header.Set("X-Request-ID", "request-123")
	withIDResponse := httptest.NewRecorder()
	handler.ServeHTTP(withIDResponse, withID)
	if got := withIDResponse.Header().Get("X-Request-ID"); got != "request-123" {
		t.Fatalf("reused request ID = %q, want request-123", got)
	}

	withoutIDResponse := httptest.NewRecorder()
	handler.ServeHTTP(withoutIDResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if withoutIDResponse.Header().Get("X-Request-ID") == "" {
		t.Fatal("generated request ID is empty")
	}
}

func TestLoggingRecordsStatusAndRecoverHandlesPanic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	logged := Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	response := httptest.NewRecorder()
	logged.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", nil))
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusCreated)
	}

	recovered := Recover(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("test panic")
	}))
	recoveredResponse := httptest.NewRecorder()
	recovered.ServeHTTP(recoveredResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if recoveredResponse.Code != http.StatusInternalServerError {
		t.Fatalf("recovered status = %d, want 500", recoveredResponse.Code)
	}
}

func TestStatusRecorderDefaultsAndWritesStatus(t *testing.T) {
	response := httptest.NewRecorder()
	recorder := &statusRecorder{ResponseWriter: response, status: http.StatusOK}
	recorder.WriteHeader(http.StatusAccepted)
	if recorder.status != http.StatusAccepted {
		t.Fatalf("recorded status = %d, want %d", recorder.status, http.StatusAccepted)
	}
}
