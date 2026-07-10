package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"time"

	"github.com/lib/pq"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/logger"
)

const (
	maxDBRetries   = 3
	retryBaseDelay = 50 * time.Millisecond
)

func withRetry[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	var zero T
	var lastErr error

	for attempt := range maxDBRetries {
		if attempt > 0 {
			delay := retryBaseDelay * time.Duration(1<<uint(attempt-1))
			logger.Debug().Int("attempt", attempt).Dur("delay", delay).Msg("retrying db operation after transient error")
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			case <-time.After(delay):
			}
		}

		result, err := fn()
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !isRetryableError(err) {
			return zero, err
		}
		logger.Warn().Err(err).Int("attempt", attempt).Msg("retryable db error")
	}

	logger.Warn().Err(lastErr).Int("attempts", maxDBRetries).Msg("db operation failed after max retries")
	return zero, lastErr
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code.Class() == "08" {
		return true
	}

	return false
}
