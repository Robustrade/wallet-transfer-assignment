# Implementation Summary

This document summarizes the key design decisions, guarantees, and operational notes for the Wallet Transfer Service implementation in this repository. It is written to help reviewers quickly validate correctness, safety, and production-readiness trade-offs.

## High-level architecture
- Clean layered design: transport → application (services) → repository → domain models.
- Single-process HTTP API exposing idempotent `POST /transfers`, wallet management, and reconciliation endpoints.
- PostgreSQL is used as the authoritative data store with the following logical tables: `wallets`, `transfers`, `ledger_entries`, and `idempotency_records`.

## Key guarantees and rationale
- Exactly-once semantics at the API level when an `idempotencyKey` is provided: repeated requests return the same logical transfer result and do not create duplicate transfers.
- Double-entry ledger: every successful transfer creates exactly two ledger entries (one `DEBIT` and one `CREDIT`) tied to the same `transfer_id`.
- Stored wallet balance for fast reads plus ledger-based reconciliation for auditability. The code updates wallets within the same transaction that writes ledger entries and transfer records to maintain strong consistency.
- Concurrency control: deterministic locking order + `SELECT ... FOR UPDATE` is used to lock both wallets inside a database transaction to avoid deadlocks and prevent double-spend races.

## Idempotency model
- Clients provide an `idempotencyKey` and a `requestHash` (hash of the request body). The server stores `idempotency_records` with `status` and the canonical `response_json` for exact replay.
- If a new request arrives with an existing key but a different payload hash, the server returns an idempotency conflict.
- The implementation uses a primary lookup in `idempotency_records` with a fallback to the `transfers` table to support deployments where idempotency records may be absent for older transfers.

Notes for reviewers: a recommended improvement (described in PR notes) is to pre-claim idempotency keys with `status = PENDING` to make claim semantics explicit and faster under high contention. The current implementation is correct and covered by integration tests, and the fallback behavior avoids silent loss of data if a record is missing.

## Schema highlights
- `wallets`: stores current `balance` and `version` (optimistic-version is present; `FOR UPDATE` is used by default but `version` remains available for optimistic strategies).
- `transfers`: stores `idempotency_key` with a unique constraint to prevent duplicate logical transfers.
- `ledger_entries`: immutable audit log, trigger-enforced invariant (deferrable) ensures that for any `transfer_id` debit and credit sums match.
- `idempotency_records`: stores `request_hash`, `status`, `response_json`, and `status_code` for exact response replay.

## Testing and correctness
- The repository includes focused integration tests that exercise: concurrent idempotent retries, locking behavior, ledger & idempotency insertion, and reconciliation helpers.
- Concurrency tests simulate simultaneous callers asserting only one transfer row is created and wallet balances are consistent.

## Observability and operational notes
- Prometheus metrics and structured JSON logging are wired into the HTTP middleware. Key metrics include request counts, durations, transfer durations, error counts, and idempotency hits.
- Health endpoints: `/health/live` and `/health/ready`. Metrics endpoint is exposed at `/metrics` (via Prometheus registry in `internal/observability`).

## Security
- SQL is parameterized across repository methods to protect against injection.
- The assignment does not include authentication/authorization by design — mandate to reviewers: secure this endpoint before production deployment (mutual TLS, API keys, OAuth, and rate limiting as appropriate).

## How to run tests locally
1. Ensure a PostgreSQL instance is available and `DATABASE_URL` (or `PGX_TEST_CONN_STRING`) is set in the environment.

```bash
export DATABASE_URL=postgres://user:pass@localhost:5432/postgres?sslmode=disable
go test ./... -v
```

Integration tests create isolated PostgreSQL schemas at runtime, so they are safe to run on shared test databases.

## What to look for in code review
- Correct transactional boundaries (wallet `FOR UPDATE`, ledger + transfer + idempotency in the same transaction).
- The deferrable constraint trigger that enforces ledger balance invariants in migrations.
- Integration tests that validate concurrent idempotent behavior.
