package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"wallet/internal/constants"
	"wallet/internal/dto"
	"wallet/internal/service"
)

// TestRouterErrorFallbacks pins the routing contract beyond the happy paths:
// unknown paths and wrong methods answer with the service's JSON error shape
// (never ServeMux's plain-text default), 405s advertise the methods that are
// allowed, and a handler panic still yields the standard 500 body.
func TestRouterErrorFallbacks(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// The store is nil on purpose: these requests never reach it, except the
	// panic case, which is exactly what it is here to exercise.
	router := NewRouter(service.New(nil, logger), logger, nil)

	cases := []struct {
		name      string
		method    string
		path      string
		want      int
		wantCode  string
		wantAllow string
	}{
		{"unknown legacy path", http.MethodGet, "/nope", http.StatusNotFound, constants.CodeNotFound, ""},
		{"unknown legacy path, other method", http.MethodPost, "/nope", http.StatusNotFound, constants.CodeNotFound, ""},
		{"unknown versioned path", http.MethodGet, "/v1/nope", http.StatusNotFound, constants.CodeNotFound, ""},
		{"wrong method on legacy collection", http.MethodGet, "/transfers", http.StatusMethodNotAllowed, constants.CodeMethodNotAllowed, "POST"},
		{"wrong method on versioned collection", http.MethodGet, "/v1/transfers", http.StatusMethodNotAllowed, constants.CodeMethodNotAllowed, "POST"},
		{"wrong method on legacy subresource", http.MethodDelete, "/wallets/wallet_1/ledger", http.StatusMethodNotAllowed, constants.CodeMethodNotAllowed, "GET, HEAD"},
		{"wrong method on versioned subresource", http.MethodDelete, "/v1/wallets/wallet_1/ledger", http.StatusMethodNotAllowed, constants.CodeMethodNotAllowed, "GET, HEAD"},
		{"panic recovered", http.MethodGet, "/wallets/wallet_1", http.StatusInternalServerError, constants.CodeInternal, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body)
			}
			if got := rec.Header().Get("Allow"); got != tc.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tc.wantAllow)
			}
			var resp dto.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode error body: %v (body %s)", err, rec.Body)
			}
			if resp.Error.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", resp.Error.Code, tc.wantCode)
			}
		})
	}

	// Both health URLs route to the same handler and answer with the health
	// shape when no database probe is configured.
	for _, path := range []string{"/healthz", "/v1/healthz"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: status = %d, want 200 (body %s)", path, rec.Code, rec.Body)
			}
			if body := rec.Body.String(); body != `{"status":"ok"}` {
				t.Errorf(`GET %s body = %s, want {"status":"ok"}`, path, body)
			}
		})
	}
}
