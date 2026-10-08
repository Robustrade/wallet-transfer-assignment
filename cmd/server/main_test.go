package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestDeadlineAndCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	var received context.Context
	handler := withRequestTimeout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Context()
		deadline, ok := received.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Fatal("request deadline missing")
		}
		cancel()
		select {
		case <-received.Done():
		case <-time.After(time.Second):
			t.Fatal("client cancellation not propagated")
		}
	}), time.Second)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil).WithContext(parent))
	if received.Err() != context.Canceled {
		t.Fatalf("unexpected context state: %v", received.Err())
	}
}
func TestRequestDeadlineExpires(t *testing.T) {
	handler := withRequestTimeout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			if r.Context().Err() != context.DeadlineExceeded {
				t.Fatal("wrong cancellation cause")
			}
		case <-time.After(time.Second):
			t.Fatal("request work was not bounded")
		}
	}), time.Millisecond)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}
