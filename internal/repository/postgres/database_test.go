package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"wallet-transfer-assignment/internal/appErrors"
)

func TestOpenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db, err := open(ctx, "postgres://127.0.0.1:1/wallet?sslmode=disable", time.Second)
	if db != nil || !errors.Is(err, appErrors.ErrDatabaseConnection) {
		t.Fatalf("expected connection failure, got %v", err)
	}
}

func TestOpenInvalidDSN(t *testing.T) {
	db, err := open(context.Background(), "://invalid", time.Second)
	if db != nil || !errors.Is(err, appErrors.ErrDatabaseOpen) {
		t.Fatalf("expected connection/configuration failure, got %v", err)
	}
}

func TestInitAllowsExplicitRetryAfterFailure(t *testing.T) {
	_, firstErr := Init(context.Background(), "://invalid", time.Second)
	if !errors.Is(firstErr, appErrors.ErrDatabaseOpen) {
		t.Fatalf("expected invalid DSN error: %v", firstErr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var callers sync.WaitGroup
	for range 20 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			// Each explicit retry gets its own connection error, not the earlier
			// DSN parsing failure. Cancellation avoids needing a running database.
			db, err := Init(ctx, "postgres://127.0.0.1:1/wallet", time.Second)
			if db != nil || !errors.Is(err, appErrors.ErrDatabaseConnection) || errors.Is(err, firstErr) {
				t.Errorf("initialization failure prevented an explicit retry: %v", err)
			}
		}()
	}
	callers.Wait()
}
