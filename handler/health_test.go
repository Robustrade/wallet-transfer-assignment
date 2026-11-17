package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/handler"
)

func TestHealth(t *testing.T) {
	t.Parallel()
	h := handler.New(nil)
	mux := http.NewServeMux()
	h.Routes(mux)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"status"`) || !strings.Contains(body, `"ok"`) {
		t.Fatalf("body=%s", body)
	}
}
