package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
	appErrors "wallet-transfer-assignment/internal/appErrors"
)

func TestRunRejectsMissingDSN(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if err := run(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil))); !errors.Is(err, appErrors.ErrDatabaseURLRequired) {
		t.Fatalf("got %v", err)
	}
}

func TestErrorCategoryDoesNotExposeCause(t *testing.T) {
	err := errors.Join(appErrors.ErrListen, errors.New("secret connection details"))
	if got := errorCategory(err); got != appErrors.ErrListen.Error() {
		t.Fatalf("unsafe category: %q", got)
	}
}

func TestServeDrainsActiveRequest(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	srv := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, srv, listener, time.Second) }()
	clientDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		request, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+listener.Addr().String(), nil)
		if requestErr != nil {
			clientDone <- requestErr
			return
		}
		resp, requestErr := client.Do(request)
		if requestErr == nil {
			_, requestErr = io.Copy(io.Discard, resp.Body)
			closeErr := resp.Body.Close()
			if requestErr == nil {
				requestErr = closeErr
			}
		}
		clientDone <- requestErr
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("returned before draining: %v", err)
	default:
	}
	close(release)
	if err := <-clientDone; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServeShutdownDeadline(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	srv := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(cancelled) })}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, srv, listener, 20*time.Millisecond) }()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		client := &http.Client{Timeout: time.Second}
		request, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+listener.Addr().String(), nil)
		if requestErr != nil {
			return
		}
		resp, requestErr := client.Do(request)
		if requestErr == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, appErrors.ErrShutdown) {
			t.Fatalf("expected shutdown deadline error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown unbounded")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("request not cancelled")
	}
	<-clientDone
}
