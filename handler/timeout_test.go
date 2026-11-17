package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWithRequestTimeout_CancelsContext(t *testing.T) {
	var sawDeadline bool
	h := WithRequestTimeout(20*time.Millisecond, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			sawDeadline = errors.Is(r.Context().Err(), context.DeadlineExceeded)
		case <-time.After(200 * time.Millisecond):
		}
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if !sawDeadline {
		t.Fatal("expected request context to deadline")
	}
}

func TestWriteServiceError_Timeout(t *testing.T) {
	rr := httptest.NewRecorder()
	writeServiceError(rr, context.DeadlineExceeded, func() {
		t.Fatal("fallback should not run")
	})
	if rr.Code != http.StatusGatewayTimeout {
		t.Fatalf("status=%d", rr.Code)
	}
}
