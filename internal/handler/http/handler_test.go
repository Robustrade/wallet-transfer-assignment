package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	handler := New(nil)
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/health", 200},
		{http.MethodPost, "/health", 405},
		{http.MethodGet, "/transfers", 405},
		{http.MethodGet, "/health/extra", 404},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("got %d, want %d", w.Code, tc.status)
			}
			if tc.status == 200 && (w.Body.String() != "{\"status\":\"ok\"}\n" || w.Header().Get("Content-Type") != "application/json") {
				t.Fatalf("unexpected health response: %v %s", w.Header(), w.Body.String())
			}
			if tc.status == 405 && w.Header().Get("Allow") == "" {
				t.Fatal("missing Allow header")
			}
		})
	}
}
