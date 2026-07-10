package middleware

import (
	"net/http"

	"github.com/loopinnovates/wallet-transfer-assignment/pkg/construct"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/logger"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/utils"
)

// ConcurrencyLimit bounds how many requests are processed at once. Without
// it, a burst far larger than the DB connection pool (e.g. thousands of
// simultaneous requests against a pool of a few dozen connections) would
// still be accepted and would just queue inside database/sql waiting for a
// connection, each one blocking for up to its own request timeout before
// failing anyway. Rejecting immediately with 503 once at capacity is fast,
// cheap (no DB touched) backpressure instead of slow, wasted work that
// fails regardless.
func ConcurrencyLimit(max int) func(http.Handler) http.Handler {
	sem := make(chan struct{}, max)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				next.ServeHTTP(w, r)
			default:
				logger.Warn().Str("path", r.URL.Path).Int("max_concurrent", max).Msg("rejecting request, server at max concurrency")
				utils.WriteError(w, construct.ErrServerBusy)
			}
		})
	}
}
