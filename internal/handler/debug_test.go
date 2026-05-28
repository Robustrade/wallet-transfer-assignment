package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNewDebugMux_ServesPprofIndex confirms the debug mux registers
// the standard pprof index route. The index returns HTML linking to
// the individual profiles; a 200 here means the registration is
// wired correctly.
func TestNewDebugMux_ServesPprofIndex(t *testing.T) {
	srv := httptest.NewServer(NewDebugMux())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/debug/pprof/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

// TestNewDebugMux_RegistersStandardProfiles spot-checks that the
// individual profile endpoints are wired (not just the index). The
// symbol endpoint is the cheapest probe — it returns a small text
// body without actually capturing a CPU profile, so we can verify
// registration without paying the 30-second profile cost.
func TestNewDebugMux_RegistersStandardProfiles(t *testing.T) {
	srv := httptest.NewServer(NewDebugMux())
	defer srv.Close()

	// Symbol endpoint without args returns a small text body — cheap to
	// probe without actually capturing a CPU profile.
	resp, err := http.Get(srv.URL + "/debug/pprof/symbol")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

// TestNewDebugMux_ServesPrometheusMetrics confirms the debug mux
// exposes /metrics in Prometheus exposition format. The metrics
// endpoint shares the security stance of pprof — admin-only.
func TestNewDebugMux_ServesPrometheusMetrics(t *testing.T) {
	srv := httptest.NewServer(NewDebugMux())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	out := string(body[:n])
	if !strings.Contains(out, "wallet_") && !strings.Contains(out, "# HELP") {
		t.Fatalf("metrics output looks wrong: %q", out)
	}
}

// TestNewDebugMux_DoesNotServeAppRoutes is a SECURITY isolation
// check. The debug mux must NOT serve any application route — if
// somebody accidentally wired both muxes into the same listener,
// /transfers and /wallets would suddenly be reachable on the admin
// port. Hitting application paths against the debug mux should
// produce only the default 404 page, never a real API response.
func TestNewDebugMux_DoesNotServeAppRoutes(t *testing.T) {
	srv := httptest.NewServer(NewDebugMux())
	defer srv.Close()

	for _, path := range []string{"/transfers", "/wallets", "/healthz"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		// Anything that prefixes with /debug/pprof/ might match pprof.Index,
		// but app paths must not.
		if resp.StatusCode == http.StatusOK {
			body := make([]byte, 256)
			_, _ = resp.Body.Read(body)
			resp.Body.Close()
			if !strings.Contains(string(body), "404") {
				t.Fatalf("debug mux served app path %s with status %d", path, resp.StatusCode)
			}
		}
		resp.Body.Close()
	}
}
