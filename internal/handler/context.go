package handler

import (
	"context"
	"time"
)

// NewContextWithTimeout creates a context with timeout from a parent context
func NewContextWithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, timeout)
}
