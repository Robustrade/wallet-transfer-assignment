# Observability

## Current implementation

The HTTP transfer handler uses Go's standard-library `log/slog` package to emit structured logs after the service returns a result.

Each transfer attempt that reaches the service produces one event:

- `INFO` — request completed successfully;
- `WARN` — request rejected with a deterministic/client-facing error;
- `ERROR` — unexpected internal or persistence failure.

The log record includes:

| Attribute | Purpose |
| --- | --- |
| `outcome` | `PROCESSED`, `FAILED`, or `REJECTED` |
| `http_status` | HTTP status returned by the handler |
| `transfer_id` | Transfer ID when one was created |
| `idempotency_key_hash` | SHA-256 digest used for correlation without logging the raw key |
| `from_wallet_id`, `to_wallet_id` | Wallets involved in the request |
| `amount` | Integer amount in the smallest currency unit |
| `failure_code` | Machine-readable business failure code, when present |
| `error_code` | Stable API error code, when present |
| `duration_ms` | Time spent executing the transfer service call |
| `error` | Underlying error for unexpected failures only |

The raw idempotency key is never logged. Internal error details are retained in server logs for diagnosis but are not returned to API clients. The handler does not log malformed requests rejected before the service is invoked.

Logs are written through the default Go `slog` logger to standard error. The default text handler is suitable for local development; a deployment can configure a JSON `slog` handler at process startup without changing transfer logic.

## Health and lifecycle

- `GET /healthz` pings PostgreSQL and returns `200` when the database is reachable or `503` when it is unavailable.
- Startup fails if database configuration, connectivity, or migrations fail.
- SIGINT and SIGTERM trigger graceful HTTP shutdown with a 10-second timeout.
- Database pool settings are configurable through the environment; see [database.md](database.md) and [run-local.md](run-local.md).

## Operational limitations

Metrics, distributed tracing, request IDs, alerting, log aggregation, and dashboards are not implemented. They are optional for this assignment. Potential future metrics include transfer outcomes by status/failure code, idempotency replays/conflicts, database transaction failures, and transfer execution duration.

Do not add raw idempotency keys, credentials, database connection strings, or full request bodies to logs.
