// Package metrics owns the project's Prometheus collectors and the
// HTTP middleware that records per-request metrics. The collectors
// are registered against a private *prometheus.Registry exposed as
// the Registry variable; tests can use it directly. Mount Handler()
// at /metrics on the ADMIN listener — never on the public API port.
package metrics
