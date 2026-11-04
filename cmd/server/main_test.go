package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakePinger struct {
	err   error
	calls int
}

func (p *fakePinger) PingContext(context.Context) error {
	p.calls++
	return p.err
}

func TestHealthHandlerReturnsOKWhenDatabaseIsAvailable(t *testing.T) {
	db := &fakePinger{}
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	healthHandler(db).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", response.Header().Get("Content-Type"))
	}
	if db.calls != 1 {
		t.Fatalf("PingContext calls = %d, want 1", db.calls)
	}
}

func TestHealthHandlerReturnsUnavailableWhenDatabasePingFails(t *testing.T) {
	db := &fakePinger{err: errors.New("connection refused")}
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	healthHandler(db).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if response.Body.String() != "database unavailable\n" {
		t.Fatalf("body = %q", response.Body.String())
	}
}

func TestHealthHandlerRejectsNonGetMethods(t *testing.T) {
	db := &fakePinger{}
	request := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	response := httptest.NewRecorder()

	healthHandler(db).ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("Allow = %q, want GET", response.Header().Get("Allow"))
	}
	if db.calls != 0 {
		t.Fatalf("PingContext calls = %d, want 0", db.calls)
	}
}
