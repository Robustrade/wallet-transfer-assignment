package service

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// retryPolicy retries a transactional operation only on Postgres serialization
// failures (40001) and deadlock detections (40P01). With READ COMMITTED + row
// locks these are rare, but retrying them cheaply keeps the worker robust.
type retryPolicy struct {
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
}

func defaultRetryPolicy() retryPolicy {
	return retryPolicy{maxRetries: 3, baseDelay: 20 * time.Millisecond, maxDelay: 500 * time.Millisecond}
}

func (r retryPolicy) do(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 0; ; attempt++ {
		if err = fn(); err == nil || !isRetryable(err) || attempt >= r.maxRetries {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.backoff(attempt)):
		}
	}
}

// backoff is exponential with full jitter to avoid synchronized retries across
// instances. math/rand/v2 is randomly seeded per process, so the jitter differs
// between processes.
func (r retryPolicy) backoff(attempt int) time.Duration {
	d := min(r.baseDelay<<attempt, r.maxDelay)
	return time.Duration(rand.Int64N(int64(d) + 1))
}

func isRetryable(err error) bool {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code == "40001" || pg.Code == "40P01"
	}
	return false
}
