# Metrics & observability

Actuator + Micrometer Prometheus metrics for the wallet-transfer service.

## Endpoints

| Endpoint | Purpose |
|---|---|
| `GET /actuator/health` | Liveness/readiness |
| `GET /actuator/metrics` | Meter names |
| `GET /actuator/prometheus` | Prometheus scrape |

## HTTP metrics (auto)

Spring Boot records `http.server.requests` (timer) with method/uri/status/outcome.

Useful PromQL:

```promql
# Throughput (req/s)
rate(http_server_requests_seconds_count{uri="/transfers",method="POST"}[1m])

# Error rate
rate(http_server_requests_seconds_count{uri="/transfers",status=~"5.."}[1m])

# Latency p95
histogram_quantile(0.95, sum(rate(http_server_requests_seconds_bucket{uri="/transfers"}[5m])) by (le))
```

## Transfer business metrics

| Meter | Type | Meaning |
|---|---|---|
| `wallet.transfer.requests` | Counter | Throughput base (`rate(..._total[1m])`) |
| `wallet.transfer.errors{error_code}` | Counter | Business/unexpected errors |
| `wallet.transfer.latency{outcome}` | Timer | End-to-end transfer processing latency |
| `wallet.transfer.outcomes{outcome}` | Counter | NEW_PROCESSED, REPLAY_PROCESSED, RETRY_PROCESSED, RESUME_PROCESSED, INSUFFICIENT_BALANCE, … |
| `wallet.transfer.amount` | Distribution summary | Amounts for newly completed successes |
| `wallet.transfer.retries` | Counter | Entries into FAILED→RETRY flow |
| `wallet.transfer.insufficient_balance` | Counter | Insufficient-balance rejects |
| `wallet.transfer.idempotency_conflicts` | Counter | Idempotency payload conflicts |
| `wallet.transfer.unique_key_races` | Counter | Concurrent unique(`idempotency_key`) insert races (recovered via replay) |

```promql
rate(wallet_transfer_requests_total[1m])
rate(wallet_transfer_errors_total[1m])
histogram_quantile(0.95, sum(rate(wallet_transfer_latency_seconds_bucket[5m])) by (le))
```

## OpenTelemetry tracing

Dependencies: `micrometer-tracing-bridge-otel` + `opentelemetry-exporter-logging`.

- Sampling: `management.tracing.sampling.probability=1.0` (all requests)
- Spans exported to **application logs** via `LoggingSpanExporter` (`OpenTelemetryTracingConfig`)
- Log lines include MDC `traceId` / `spanId`
- Custom span: `wallet.transfer` (Micrometer Observation) under the HTTP server span

Typical transfer trace tree:

```
http POST /transfers
└── wallet.transfer
```
