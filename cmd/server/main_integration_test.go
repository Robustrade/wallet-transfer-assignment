//go:build integration

package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/config"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/testdb"
)

// TestRun_GracefulShutdown drives the full main.run lifecycle:
//
//  1. start run() in a goroutine with a cancellable context,
//  2. wait until /healthz responds 200 (server is up),
//  3. cancel the context to simulate SIGINT/SIGTERM,
//  4. assert run returned without an unexpected error within 15s.
//
// This is the only test that exercises the goroutine + signal +
// graceful-shutdown plumbing in main; if the shutdown path ever
// deadlocked, the 15-second guard would fail this test fast.
func TestRun_GracefulShutdown(t *testing.T) {
	_ = testdb.Get(t) // ensure container started
	testdb.Reset(t)

	addr := freePort(t)
	cfg := config.Config{
		DatabaseURL: testdb.DSN(t),
		HTTPAddr:    addr,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- run(ctx, cfg)
	}()

	// Wait for the listener to come up.
	if !waitForHealthz("http://"+addr+"/healthz", 5*time.Second) {
		t.Fatal("server did not become ready in time")
	}

	cancel() // trigger graceful shutdown

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("run did not return after cancel")
	}
}

// TestRun_BadDatabaseURL verifies the fail-fast contract: if the
// DSN points at an unreachable host (here: discard port on
// loopback with a 1-second timeout), run must return an error
// instead of producing a *sql.DB that explodes later on the first
// query. In a deployment this becomes "the pod restart-loops with
// a clear error log" rather than "the pod is up but everything
// returns 500".
func TestRun_BadDatabaseURL(t *testing.T) {
	cfg := config.Config{
		DatabaseURL: "postgres://nobody:nobody@127.0.0.1:1/none?sslmode=disable&connect_timeout=1",
		HTTPAddr:    ":0",
	}
	err := run(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected db.Open to fail")
	}
}

// TestRun_DebugListenerServesPprof is a SECURITY ISOLATION test.
// When DEBUG_ADDR is set, run() must:
//
//  1. serve /debug/pprof/ on the debug port (200), and
//  2. NOT serve /debug/pprof/ on the public API port (must be 4xx).
//
// If step 2 ever started succeeding, pprof would be reachable from
// anyone who could reach the public API — a serious info leak. This
// test is the regression guard.
func TestRun_DebugListenerServesPprof(t *testing.T) {
	_ = testdb.Get(t)
	testdb.Reset(t)

	apiAddr := freePort(t)
	debugAddr := freePort(t) // we'll bind to whatever port; test only hits 127.0.0.1
	cfg := config.Config{
		DatabaseURL: testdb.DSN(t),
		HTTPAddr:    apiAddr,
		DebugAddr:   debugAddr,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg) }()

	if !waitForHealthz("http://"+apiAddr+"/healthz", 5*time.Second) {
		t.Fatal("api did not become ready")
	}

	// pprof must be reachable on the debug listener…
	resp, err := http.Get("http://" + debugAddr + "/debug/pprof/")
	if err != nil {
		t.Fatalf("get pprof: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pprof status: %d", resp.StatusCode)
	}

	// …and must NOT be reachable on the main API listener.
	resp2, err := http.Get("http://" + apiAddr + "/debug/pprof/")
	if err != nil {
		t.Fatalf("get pprof on api: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode == http.StatusOK {
		t.Fatalf("pprof should NOT be exposed on the API port; got %d", resp2.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("run did not return after cancel")
	}
}

// TestIsLoopbackAddr is the table-driven check for the helper that
// decides whether to emit the "pprof on non-loopback" WARN at
// startup. The cases enumerate the realistic inputs:
//
//   - explicit loopbacks (127.0.0.1, localhost, ::1) → true
//   - bare ":port" → false (binds 0.0.0.0)
//   - explicit 0.0.0.0 or any non-loopback IP → false
//   - malformed strings → false
//
// If we ever changed isLoopbackAddr to return true for ":port" we
// would silently lose the security warning.
func TestIsLoopbackAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:6060", true},
		{"localhost:6060", true},
		{"[::1]:6060", true},
		{":6060", false}, // binds 0.0.0.0
		{"0.0.0.0:6060", false},
		{"10.0.0.1:6060", false},
		{"not-an-addr", false},
	}
	for _, c := range cases {
		t.Run(c.addr, func(t *testing.T) {
			if got := isLoopbackAddr(c.addr); got != c.want {
				t.Fatalf("isLoopbackAddr(%q) = %v, want %v", c.addr, got, c.want)
			}
		})
	}
}

// TestRun_BadHTTPAddr verifies the listener-error branch of run():
// a malformed HTTP address must propagate as a returned error, not
// silently leave the listener un-started.
func TestRun_BadHTTPAddr(t *testing.T) {
	cfg := config.Config{
		DatabaseURL: testdb.DSN(t),
		HTTPAddr:    "definitely-not-an-addr",
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := run(ctx, cfg); err == nil {
		t.Fatal("expected listener to fail")
	}
}

// ---------------- helpers ----------------

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func waitForHealthz(url string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
