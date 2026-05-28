package handler

import (
	"net/http"
	"net/http/pprof"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/metrics"
)

// NewDebugMux returns an http.Handler that serves Go's standard
// runtime profiling endpoints under /debug/pprof/.
//
// What pprof exposes (and why this is sensitive):
//
//   - /debug/pprof/                  HTML index of available profiles
//   - /debug/pprof/cmdline           process arguments (may include
//     flag-derived secrets)
//   - /debug/pprof/profile           30-second CPU profile (default)
//   - /debug/pprof/symbol            symbol-name lookup
//   - /debug/pprof/trace             execution trace
//   - /debug/pprof/heap              heap profile (inherited from
//     pprof.Index)
//   - /debug/pprof/goroutine         goroutine dumps (includes stack
//     traces of every live goroutine —
//     can leak sensitive in-memory
//     data via stack frames)
//
// Because of that surface area, this mux MUST NOT be served on the
// same listener as the public API. The intended deployment is a
// dedicated listener bound to loopback (e.g. 127.0.0.1:6060) or an
// admin-only network, gated behind an opt-in DEBUG_ADDR env var. See
// cmd/server/main.go for the wiring and the loopback warning.
//
// The matching tests live in debug_test.go and an integration test in
// cmd/server confirms that the debug routes are NOT reachable from
// the public API port.
func NewDebugMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	// /metrics serves Prometheus exposition format. The same
	// "admin only" stance as pprof applies — metrics reveal
	// internal rates and timings that aid an attacker doing recon.
	mux.Handle("/metrics", metrics.Handler())
	return mux
}
