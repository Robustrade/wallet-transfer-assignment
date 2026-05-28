package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNewRouter_Healthz exercises the no-DB healthz route. It serves
// as a smoke test that the mux is wired (constructor + middleware
// chain assembled cleanly) without requiring any service or DB
// setup. The real route assertions for /transfers and /wallets live
// in the integration tests.
func TestNewRouter_Healthz(t *testing.T) {
	mux := NewRouter(nil, nil)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "ok" {
		t.Fatalf("body: %q", string(b))
	}
}

// TestNewRouter_UnknownRoute confirms that an unregistered path
// returns 404, not 200 or 500. This protects against accidentally
// catch-all routes that would mask typos in the route table.
func TestNewRouter_UnknownRoute(t *testing.T) {
	mux := NewRouter(nil, nil)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/no-such-thing")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

// TestNewRouter_WrongMethod confirms that hitting a registered path
// with the wrong method returns 405 Method Not Allowed (Go 1.22+
// method-aware ServeMux behaviour) rather than 404. This is what
// lets a client distinguish "path doesn't exist" from "method
// doesn't apply".
func TestNewRouter_WrongMethod(t *testing.T) {
	mux := NewRouter(nil, nil)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/transfers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}
