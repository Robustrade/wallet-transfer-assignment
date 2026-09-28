package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func envFrom(vars map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := vars[k]; return v, ok }
}

func TestRun_UnsupportedDriverFailsAtStartup(t *testing.T) {
	t.Parallel()
	err := run(context.Background(), envFrom(map[string]string{"DB_DRIVER": "mysql"}), io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DB_DRIVER")
}

func TestRun_UnreachableDatabaseFailsAtStartup(t *testing.T) {
	t.Parallel()
	err := run(context.Background(), envFrom(map[string]string{
		"DB_DSN": "file:" + filepath.Join(t.TempDir(), "missing-dir", "x.db"),
	}), io.Discard)
	require.Error(t, err)
}

func TestRun_ServesSeededWalletsAndShutsDownGracefully(t *testing.T) {
	t.Parallel()
	base, stop := startServer(t, map[string]string{
		"DB_DSN": "file:" + filepath.Join(t.TempDir(), "wallet.db"),
	})

	// The demo seed is applied, so a transfer between seeded wallets works.
	resp, err := request(http.MethodPost, base+"/transfers",
		`{"idempotencyKey":"boot-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	resp, err = request(http.MethodGet, base+"/readyz", "")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	require.NoError(t, stop(), "graceful shutdown returns nil")
}

// startServer runs the server in-process, exactly as main() does, on a free
// port. It waits until /healthz answers, and returns the base URL and a stop
// function. stop cancels the context (what SIGINT/SIGTERM does via
// signal.NotifyContext) and returns run's result.
//
// Picking a free port and then binding it is racy: after freePort releases
// the port, the OS may give it to an outgoing connection, such as the server's
// own database connections, which open before it listens. On "address already
// in use" we simply try another port.
func startServer(t *testing.T, env map[string]string) (base string, stop func() error) {
	t.Helper()
	for attempt := 1; attempt <= 5; attempt++ {
		vars := map[string]string{"PORT": fmt.Sprint(freePort(t))}
		for k, v := range env {
			vars[k] = v
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- run(ctx, envFrom(vars), io.Discard) }()

		base = "http://127.0.0.1:" + vars["PORT"]
		err := waitHealthy(base, done)
		if err == nil {
			return base, func() error {
				cancel()
				select {
				case err := <-done:
					return err
				case <-time.After(15 * time.Second):
					return errors.New("server did not shut down within 15s")
				}
			}
		}
		cancel()
		if !strings.Contains(err.Error(), "address already in use") {
			t.Fatalf("server did not start: %v", err)
		}
	}
	t.Fatal("no free port after 5 attempts")
	return "", nil
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// waitHealthy polls /healthz until it answers 200, or the server exits.
func waitHealthy(base string, done <-chan error) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			return fmt.Errorf("server exited early: %w", err)
		default:
		}
		resp, err := request(http.MethodGet, base+"/healthz", "")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("server never became healthy")
}

// request sends an HTTP request with a bounded context.
func request(method, url, body string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	// Read the body before cancel() runs, so the caller can still close it.
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(strings.NewReader(string(b)))
	return resp, nil
}
