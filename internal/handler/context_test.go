package handler

import (
	"context"
	"testing"
	"time"
)

func TestNewContextWithTimeout(t *testing.T) {
	ctx, cancel := NewContextWithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatalf("context should not be done immediately")
	default:
	}

	time.Sleep(20 * time.Millisecond)

	if ctx.Err() == nil {
		t.Fatalf("expected context timeout error")
	}
}
