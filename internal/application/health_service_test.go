package application

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakePool struct {
	PingFn    func(context.Context) error
	StatValue fakePoolStat
}

type fakePoolStat struct {
	totalConns           int32
	idleConns            int32
	acquiredConns        int32
	maxConns             int32
	acquireCount         int64
	emptyAcquireCount    int64
	emptyAcquireWaitTime time.Duration
}

func (f fakePoolStat) TotalConns() int32 {
	return f.totalConns
}

func (f fakePoolStat) IdleConns() int32 {
	return f.idleConns
}

func (f fakePoolStat) AcquiredConns() int32 {
	return f.acquiredConns
}

func (f fakePoolStat) MaxConns() int32 {
	return f.maxConns
}

func (f fakePoolStat) AcquireCount() int64 {
	return f.acquireCount
}

func (f fakePoolStat) EmptyAcquireCount() int64 {
	return f.emptyAcquireCount
}

func (f fakePoolStat) EmptyAcquireWaitTime() time.Duration {
	return f.emptyAcquireWaitTime
}

func (f *fakePool) Ping(ctx context.Context) error {
	if f.PingFn == nil {
		return nil
	}
	return f.PingFn(ctx)
}

func (f *fakePool) Stat() DBPoolStat {
	return f.StatValue
}

func TestHealthService_Live(t *testing.T) {
	svc := NewHealthService(&fakePool{})

	response := svc.Live(context.Background())

	if response.Status != "UP" {
		t.Fatalf("expected status UP, got %q", response.Status)
	}
}

func TestHealthService_Ready_Success(t *testing.T) {
	pool := &fakePool{
		PingFn: func(context.Context) error {
			return nil
		},
		StatValue: fakePoolStat{
			totalConns:           3,
			idleConns:            2,
			acquiredConns:        1,
			maxConns:             10,
			acquireCount:         5,
			emptyAcquireCount:    0,
			emptyAcquireWaitTime: 0,
		},
	}
	svc := NewHealthService(pool)

	response, err := svc.Ready(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if response.Status != "UP" {
		t.Fatalf("expected status UP, got %q", response.Status)
	}
	if len(response.Checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(response.Checks))
	}
	if response.Checks[0].Status != "UP" {
		t.Fatalf("expected postgres check UP, got %q", response.Checks[0].Status)
	}
}

func TestHealthService_Ready_Failure(t *testing.T) {
	errFail := errors.New("ping failed")
	pool := &fakePool{
		PingFn: func(context.Context) error {
			return errFail
		},
		StatValue: fakePoolStat{},
	}
	svc := NewHealthService(pool)

	response, err := svc.Ready(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if response.Status != "DOWN" {
		t.Fatalf("expected status DOWN, got %q", response.Status)
	}
	if len(response.Checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(response.Checks))
	}
	if response.Checks[0].Status != "DOWN" {
		t.Fatalf("expected postgres check DOWN, got %q", response.Checks[0].Status)
	}
}
