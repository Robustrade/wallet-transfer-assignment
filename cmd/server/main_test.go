package main

import (
	"net"
	"net/http"
	"testing"
	"time"
)

func TestShutdownClosesConnectionsWhenGracefulTimesOut(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan struct{})
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
			close(finished)
		}),
		ReadHeaderTimeout: time.Second,
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = server.Serve(ln)
	}()

	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		res, err := http.Get("http://" + ln.Addr().String() + "/")
		if res != nil {
			_ = res.Body.Close()
		}
		_ = err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}

	if err := shutdownServer(server, 50*time.Millisecond); err == nil {
		t.Fatal("expected shutdown to time out")
	}

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("request context was not canceled")
	}
	select {
	case <-clientDone:
	case <-time.After(2 * time.Second):
		t.Fatal("client did not return")
	}
}
