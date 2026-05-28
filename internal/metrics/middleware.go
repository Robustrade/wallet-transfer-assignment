package metrics

import (
	"net/http"
	"strconv"
	"time"
)

// HTTPMiddleware records two metrics per request: the
// HTTPRequestsTotal counter and the HTTPRequestDuration histogram.
// The middleware wraps the response writer so the final status code
// is observable.
//
// The route label comes from r.Pattern (Go 1.22+ ServeMux populates
// this with the matched route pattern, e.g. "GET /wallets/{id}").
// This is critical: labelling on r.URL.Path would explode cardinality
// the moment a path contains an id, blowing up Prometheus storage.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		// r.Pattern was added in Go 1.22 alongside method-aware mux
		// patterns. For requests that did NOT match a route (404s),
		// it will be empty — fall back to a literal "_unmatched_"
		// so we don't accidentally create a high-cardinality label.
		route := r.Pattern
		if route == "" {
			route = "_unmatched_"
		}
		status := strconv.Itoa(rec.status)
		HTTPRequestsTotal.WithLabelValues(r.Method, route, status).Inc()
		HTTPRequestDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}

// statusRecorder captures the final response status so the metrics
// middleware can label the counter correctly. Mirrors the type used
// by internal/handler.AccessLog — we duplicate (rather than import)
// to keep the metrics package free of circular dependencies on the
// handler package.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status = http.StatusOK
		s.wrote = true
	}
	return s.ResponseWriter.Write(b)
}
