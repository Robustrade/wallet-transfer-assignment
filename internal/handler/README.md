# HTTP layer

`transfers.go` exposes `POST /transfers` through the standard-library `ServeMux`.

The handler validates JSON syntax/content type/body size, maps the service's domain
errors to HTTP codes, and serializes durable `TransferResult` fields. It does not
check balances, query wallets, post ledger entries or decide idempotency outcomes.

PostgreSQL-backed HTTP integration tests live in `internal/service/http_integration_test.go`
to reuse the existing real-Postgres isolated-schema fixture and reconciliation
assertions without changing the previously verified transaction tests.
