package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"serialization failure", &pgconn.PgError{Code: "40001"}, true},
		{"deadlock detected", &pgconn.PgError{Code: "40P01"}, true},
		{"unique violation", &pgconn.PgError{Code: "23505"}, false},
		{"plain error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isRetryable(c.err); got != c.want {
				t.Fatalf("isRetryable = %v, want %v", got, c.want)
			}
		})
	}
}

func TestRetry_RetriesThenSucceeds(t *testing.T) {
	p := retryPolicy{maxRetries: 3, baseDelay: time.Millisecond, maxDelay: 2 * time.Millisecond}
	calls := 0
	err := p.do(context.Background(), func() error {
		calls++
		if calls < 3 {
			return &pgconn.PgError{Code: "40001"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestRetry_ExhaustsAndReturnsError(t *testing.T) {
	p := retryPolicy{maxRetries: 2, baseDelay: time.Millisecond, maxDelay: 2 * time.Millisecond}
	calls := 0
	err := p.do(context.Background(), func() error {
		calls++
		return &pgconn.PgError{Code: "40001"}
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if calls != 3 { // initial + 2 retries
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestRetry_NonRetryableReturnsImmediately(t *testing.T) {
	p := defaultRetryPolicy()
	calls := 0
	err := p.do(context.Background(), func() error {
		calls++
		return errors.New("boom")
	})
	if err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d, want one call and an error", err, calls)
	}
}

func TestRetry_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := retryPolicy{maxRetries: 5, baseDelay: time.Second, maxDelay: time.Second}
	err := p.do(ctx, func() error { return &pgconn.PgError{Code: "40001"} })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestBackoff_WithinBounds(t *testing.T) {
	p := retryPolicy{maxRetries: 5, baseDelay: 10 * time.Millisecond, maxDelay: 40 * time.Millisecond}
	for attempt := 0; attempt < 6; attempt++ {
		d := p.backoff(attempt)
		if d < 0 || d > p.maxDelay {
			t.Fatalf("attempt %d: backoff %v out of bounds [0,%v]", attempt, d, p.maxDelay)
		}
	}
}
