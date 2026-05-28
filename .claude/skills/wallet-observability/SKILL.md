---
name: wallet-observability
description: Use this skill any time work in this wallet-transfer repo touches observability — Prometheus metrics or OpenTelemetry tracing. Trigger when editing files under internal/metrics/ or internal/tracing/, when adding a new metric (counter/histogram/gauge), when wrapping a service method in a span, when configuring exporters, when changing what gets recorded for HTTP requests, or when the user asks to "add a metric", "add a span", "record latency", "increase observability", "instrument X", or anything about Prometheus, OpenTelemetry, OTLP, or the /metrics endpoint. Do NOT trigger for pure business-logic changes that don't touch the metrics/tracing packages, or for log-line edits (those use slog and are covered by AGENTS.md §S-6 + the existing middleware).
---

# wallet-observability

This is a financial system. Read [AGENTS.md](../../../AGENTS.md) §S-6
(don't log secrets / bodies) and §S-16 (admin-only listener for
internal-surface endpoints) before adding new metrics or spans.

The observability stack:

```
┌────────────────────────────────────────────────────────────────┐
│ Public API listener :8080                                      │
│   request → otelhttp parent span → metrics.HTTPMiddleware      │
│           → handler → service → repo → DB                      │
│                              ↘ tracing.Tracer().Start child    │
│                              ↘ metrics.TransfersTotal.Inc      │
└────────────────────────────────────────────────────────────────┘
┌────────────────────────────────────────────────────────────────┐
│ Debug listener (DEBUG_ADDR, loopback default)                  │
│   /debug/pprof/*    pprof profiles                             │
│   /metrics          Prometheus exposition                      │
└────────────────────────────────────────────────────────────────┘
```

---

## Hard rules

1. **Metrics live on the admin listener only.** Never register
   `/metrics` on the public API mux. The route check in
   [internal/handler/debug.go](../../../internal/handler/debug.go)
   is the gate; the test
   `cmd/server.TestRun_DebugListenerServesPprof` (extended) is the
   regression guard.
2. **Cardinality discipline.** Label by route *pattern* (`r.Pattern`),
   not raw URL path. Untrusted strings — wallet IDs, transfer IDs,
   user IDs — must NEVER appear as label values; each unique value
   creates a separate time series and explodes Prometheus storage.
3. **No external network in tracing exporters by default.** The
   `stdouttrace` exporter is the only one wired in. Switching to
   OTLP is a constructor swap in
   [internal/tracing/tracing.go:Setup](../../../internal/tracing/tracing.go);
   do it deliberately, with a runtime gate (env var), and never
   silently from a default.
4. **Recording happens at the boundary.** Counters/histograms are
   incremented in the service layer (`TransferService.Create`) and
   in the HTTP middleware. Don't sprinkle Prometheus calls into
   repositories — they should remain SQL-only.
5. **Spans bracket the operation, not each repository call.** One
   span per service method; one parent span per HTTP request (via
   `otelhttp`). Sub-spans inside the service are fine when the
   operation does multiple DB round-trips that are worth visualising.

---

## Templates

### A — adding a new counter

```go
// internal/metrics/metrics.go
var TransferRefundsTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "wallet_transfer_refunds_total",
        Help: "Total refunded transfers by terminal state.",
    },
    []string{"state"},
)

func init() {
    Registry.MustRegister(TransferRefundsTotal /*, …existing… */)
}
```

Use:

```go
// internal/service/refund.go
metrics.TransferRefundsTotal.WithLabelValues(string(t.State)).Inc()
```

### B — adding a histogram

Match the existing pattern: `_seconds` suffix, default Prometheus
buckets unless you genuinely know the latency profile.

```go
var RefundDuration = prometheus.NewHistogramVec(
    prometheus.HistogramOpts{
        Name:    "wallet_refund_duration_seconds",
        Help:    "End-to-end refund processing duration in seconds.",
        Buckets: prometheus.DefBuckets,
    },
    []string{"state"},
)
```

Observation:

```go
start := time.Now()
defer func() {
    RefundDuration.WithLabelValues(state).Observe(time.Since(start).Seconds())
}()
```

### C — adding a span to a service method

```go
// internal/service/refund.go
func (s *RefundService) Process(ctx context.Context, req RefundRequest) (*domain.Refund, error) {
    ctx, span := tracing.Tracer().Start(ctx, "RefundService.Process")
    defer span.End()

    span.SetAttributes(
        attribute.String("refund.transfer_id", req.TransferID.String()),
        attribute.Int64("refund.amount", req.Amount),
    )

    // ... business logic ...
    if err != nil {
        span.RecordError(err)
        return nil, err
    }
    span.SetAttributes(attribute.String("refund.state", string(out.State)))
    return out, nil
}
```

### D — verifying you didn't blow up cardinality

```go
// In a test that exercises N distinct path values, verify the
// counter only generated ONE label combination, not N.
const route = "GET /things/{id}"
for _, id := range []string{"a", "b", "c"} {
    req := httptest.NewRequest(http.MethodGet, "/things/"+id, nil)
    rec := httptest.NewRecorder()
    wrapped.ServeHTTP(rec, req)
}
v := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("GET", route, "200"))
if v < 3 {
    t.Fatalf("expected merged series, got %v — label cardinality leak?", v)
}
```

---

## Anti-patterns (will be caught at review)

| Anti-pattern | Reason | Fix |
|---|---|---|
| Mounting `/metrics` on the public mux | Exposes internal rates / process info to attackers | Mount on `NewDebugMux()` only |
| Using `r.URL.Path` as a label value | Cardinality explosion (one series per id) | Use `r.Pattern` (Go 1.22+ ServeMux) |
| Recording wallet IDs / transfer IDs / user IDs as labels | Same — unbounded cardinality | Set them as **span attributes** instead; spans handle high-cardinality data |
| New collectors registered against `prometheus.DefaultRegisterer` | Conflicts with our private Registry during tests | Use `metrics.Registry.MustRegister(...)` |
| Span without `defer span.End()` | Spans leak; trace UIs show "still in progress" forever | Always defer |
| Long-lived span that wraps a goroutine / channel send | Span lifetime != goroutine lifetime; the SDK assumes synchronous Start/End | Open the span where the work happens, not where you launched it |
| Calling `metrics.X.WithLabelValues(...).Inc()` inside a repository method | Couples persistence to observability; breaks the layering rule | Record in the service layer where the outcome is known |
| Replacing `stdouttrace` with an OTLP exporter without a runtime gate | An OTLP misconfiguration silently sends sensitive trace data over the network | Gate on env var; default off |

---

## Quick-reference: where things live

| Concern | File |
|---|---|
| Prometheus collectors + Handler() | [internal/metrics/metrics.go](../../../internal/metrics/metrics.go) |
| HTTP metrics middleware (route-pattern aware) | [internal/metrics/middleware.go](../../../internal/metrics/middleware.go) |
| OTel TracerProvider setup + Tracer() helper | [internal/tracing/tracing.go](../../../internal/tracing/tracing.go) |
| `/metrics` registration on debug listener | [internal/handler/debug.go](../../../internal/handler/debug.go) |
| `otelhttp` wrap on the API mux | [cmd/server/main.go](../../../cmd/server/main.go) — `apiHandler` construction |
| Recording outcomes in the service | [internal/service/transfer.go](../../../internal/service/transfer.go) — `Create` |

---

## Verification after editing

```sh
# Build, vet, lint
go build ./... && go vet ./... && go vet -tags=integration ./...
golangci-lint run ./... && golangci-lint run --build-tags=integration ./...
gofmt -l .

# Tests for the observability packages
go test -race ./internal/metrics/... ./internal/tracing/...

# Full suite (catches integration-side regressions)
go test -race -tags=integration -count=1 -timeout=300s ./...

# Smoke-test that /metrics serves real data after a request
DEBUG_ADDR=127.0.0.1:6060 make run &
curl -s -XPOST localhost:8080/wallets -d '{"id":"w","balance":0}'
curl -s 127.0.0.1:6060/metrics | grep wallet_
```

If anything new ships, also run `/secure-review` so the new code
goes through the security checklist before the diff is committed.
