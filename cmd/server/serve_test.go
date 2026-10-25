package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var discardLog = slog.New(slog.NewTextHandler(io.Discard, nil))

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	return ln
}

// A handler stuck on a database row lock only returns once its request context
// is cancelled. If shutdown merely gives up after the grace period, that
// handler keeps its pooled connection and pool.Close() blocks process exit.
func TestServe_CancelsStuckRequestsWhenGracefulShutdownTimesOut(t *testing.T) {
	started := make(chan struct{})
	released := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done() // models a transaction blocked on a row lock
		close(released)
	})}
	ln := listen(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	served := make(chan error, 1)
	go func() { served <- serve(ctx, srv, ln, 100*time.Millisecond, discardLog) }()

	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached the handler")
	}

	stop() // SIGTERM

	select {
	case err := <-served:
		require.Error(t, err, "a forced shutdown is reported, not silent")
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after the grace period")
	}
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("the stuck handler's request context was never cancelled, so its database connection would never be released")
	}
}

func TestServe_LetsInFlightRequestsFinishWithinTheGracePeriod(t *testing.T) {
	started := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	})}
	ln := listen(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	served := make(chan error, 1)
	go func() { served <- serve(ctx, srv, ln, 5*time.Second, discardLog) }()

	status := make(chan int, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err != nil {
			status <- 0
			return
		}
		_ = resp.Body.Close()
		status <- resp.StatusCode
	}()
	<-started
	stop()

	assert.Equal(t, http.StatusNoContent, <-status, "the in-flight request completes normally")
	assert.NoError(t, <-served, "a clean drain is not an error")
}

func TestServe_StopsCleanlyWhenIdle(t *testing.T) {
	srv := &http.Server{Handler: http.NotFoundHandler()}
	ln := listen(t)
	ctx, stop := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- serve(ctx, srv, ln, time.Second, discardLog) }()

	stop()

	select {
	case err := <-served:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return")
	}
}
