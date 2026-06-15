# Wallet Transfer Service

This is a Go implementation of the wallet transfer assignment. It provides a small HTTP service with SQLite persistence, idempotent transfer creation, stored wallet balances, and double-entry ledger rows.

## Architecture

- `cmd/server`: executable HTTP server.
- `internal/httpapi`: thin HTTP handlers and response mapping.
- `internal/service`: transfer workflow, idempotency, state transitions, and business rules.
- `internal/repository`: SQLite schema, transactions, and persistence operations.
- `internal/domain`: domain models, error codes, and validation.
- `docs/design.md`: behavior, failure modes, idempotency, concurrency, and testing notes.

## API

Create wallets for setup:

```bash
curl -X POST http://localhost:8080/wallets \
  -H 'Content-Type: application/json' \
  -d '{"id":"wallet_1","initialBalance":1000}'

curl -X POST http://localhost:8080/wallets \
  -H 'Content-Type: application/json' \
  -d '{"id":"wallet_2","initialBalance":0}'
```

Create a transfer:

```bash
curl -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

Read state:

```bash
curl http://localhost:8080/wallets/wallet_1
curl http://localhost:8080/transfers/<transfer_id>
curl http://localhost:8080/health
```

## Persistence

SQLite is used for a self-contained submission. The schema includes:

- `wallets` with non-negative stored balances.
- `transfers` with a unique `idempotency_key`, request hash, and `PENDING`, `PROCESSED`, or `FAILED` state.
- `ledger_entries` with one `DEBIT` and one `CREDIT` row per processed transfer.

The service runs each transfer in a `BEGIN IMMEDIATE` transaction and uses an atomic guarded debit update to prevent double spending.

## Idempotency

`POST /transfers` stores a normalized request hash with the idempotency key. Repeating the same request returns the persisted original result and does not create another transfer or ledger pair. Reusing the key with different transfer parameters returns `409 idempotency_key_conflict`.

## How to Run

```bash
go run ./cmd/server
```

Optional environment variables:

- `PORT`: HTTP port, default `8080`.
- `DATABASE_DSN`: SQLite DSN, default `file:wallet.db?_busy_timeout=5000&_foreign_keys=on`.

## How to Test

```bash
gofmt -w .
go test ./...
go test ./... -race -cover
```

CI runs these Go commands by default. Repository variables can override them if maintainers want different commands:

- `LINT_CMD=golangci-lint run ./...`
- `FORMAT_CHECK_CMD=test -z "$(gofmt -l .)"`
- `TEST_CMD=go test ./... -race -cover`
