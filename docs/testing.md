# Testing

Tests are behavioral. They assert balances, ledger rows, HTTP status, and returned transfer ids. They do not assert SQL text or private helper calls.

## Layers

| Layer | What it proves | Where |
| --- | --- | --- |
| Domain | Amount and id rules. Legal and illegal state transitions. Ledger pair shape. | `internal/domain` |
| Use case | Idempotent replay, payload mismatch, insufficient funds, wallet absence, successful posting. Uses an in-memory port with rollback. | `internal/app/transfer`, `internal/app/wallet` |
| HTTP | Status codes and JSON for validation, conflicts, not found, processed, and failed transfers. Real use case, fake port. | `internal/adapter/httpapi` |
| Config | Missing password, password rejected in YAML, environment overrides, `DATABASE_URL`. | `internal/platform/config` |
| Postgres | Schema triggers, concurrency, double spend, deadlock avoidance, idempotent replay under parallel requests, reconciliation. | `internal/integration` |

The in-memory port serializes a whole transaction with a mutex. That is enough to test use-case branching. It is not evidence of row-lock safety. Concurrency tests run against PostgreSQL.

## How PostgreSQL is started for tests

`internal/testutil/postgres_test.go` chooses, in order:

1. `WALLET_TEST_DATABASE_URL`, if set. Tests share that database and truncate between cases.
2. A temporary cluster started with local `initdb` and `pg_ctl`.

The binary directory is `WALLET_POSTGRES_BIN`, then `initdb` on `PATH`, then Homebrew paths for PostgreSQL 16, 15, and 14.

Each temporary-cluster test gets its own database cloned from a migrated template, so cases do not share rows.

If neither a DSN nor server binaries are available, integration tests fail with setup instructions rather than passing silently.

## Commands

```bash
go test ./...
go test -race ./...
go vet ./...
make cover
```

`go test -short ./...` skips the integration package's database cases.

## What the concurrency tests do

- Many goroutines debit one wallet with distinct idempotency keys. Succeeded amounts sum to the original balance. The balance never goes negative. Ledger debits equal credits.
- Many goroutines submit the same idempotency key. One transfer id is returned by every response. The balance changes once.
- Opposite transfers (`A -> B` and `B -> A`) run together and finish. Wallet locks are taken in id order, so this must not deadlock.
- A direct SQL commit of a `PROCESSED` transfer without a balanced ledger is rejected by the deferred trigger.
