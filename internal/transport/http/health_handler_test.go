package transporthttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/application"
)

type fakePoolStat struct {
	totalConns int32
	idleConns  int32
}

func (f fakePoolStat) TotalConns() int32 {
	return f.totalConns
}

func (f fakePoolStat) IdleConns() int32 {
	return f.idleConns
}

func (f fakePoolStat) AcquiredConns() int32 {
	return 0
}

func (f fakePoolStat) MaxConns() int32 {
	return 0
}

func (f fakePoolStat) AcquireCount() int64 {
	return 0
}

func (f fakePoolStat) EmptyAcquireCount() int64 {
	return 0
}

func (f fakePoolStat) EmptyAcquireWaitTime() time.Duration {
	return 0
}

type fakePool struct {
	PingFn    func(context.Context) error
	StatValue fakePoolStat
}

func (f *fakePool) Ping(ctx context.Context) error {
	return f.PingFn(ctx)
}

func (f *fakePool) Stat() application.DBPoolStat {
	return f.StatValue
}

func newServerWithHealthPool(pool *fakePool) *Server {
	return NewServer(nil, nil, application.NewHealthService(pool))
}

func TestHandleLive_ReturnsUp(t *testing.T) {
	server := newServerWithHealthPool(&fakePool{})
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.Code)
	}

	var response application.HealthResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.Status != "UP" {
		t.Fatalf("expected status UP, got %q", response.Status)
	}
}

func TestHandleReady_ReturnsOkWhenPoolHealthy(t *testing.T) {
	pool := &fakePool{
		PingFn: func(context.Context) error {
			return nil
		},
		StatValue: fakePoolStat{},
	}
	server := newServerWithHealthPool(pool)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.Code)
	}

	var response application.HealthResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.Status != "UP" {
		t.Fatalf("expected status UP, got %q", response.Status)
	}
}

func TestHandleReady_ReturnsServiceUnavailableWhenPoolUnhealthy(t *testing.T) {
	pool := &fakePool{
		PingFn: func(context.Context) error {
			return errors.New("connection refused")
		},
		StatValue: fakePoolStat{},
	}
	server := newServerWithHealthPool(pool)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", res.Code)
	}

	var response application.HealthResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.Status != "DOWN" {
		t.Fatalf("expected status DOWN, got %q", response.Status)
	}
}
