# Wallet Transfer Service

A Go/Gin service implementing idempotent wallet-to-wallet transfers with
a double-entry ledger. Correctness, transactional safety, and clean
layering take priority over feature breadth.

- **Idempotent by construction** — a two-step `init` → `transfer` flow
  where the minted Snowflake ID doubles as the idempotency key, PK, and
  ledger FK target.
- **Concurrency-safe** — wallet locks acquired in a fixed order, verified
  under real contention by a concurrency stress test and a double-click
  race test, both run with the Go race detector.
- **Double-entry ledger** — exactly two `ledger_entries` rows (one DEBIT,
  one CREDIT) per processed transfer, written only when the transfer
  actually succeeds.
- **Typed domain errors** mapped to precise HTTP status codes, not a
  generic 4xx/5xx catch-all.

Full design rationale — schema, the idempotency/locking flow, every
considered and rejected alternative — lives in
[docs/DESIGN.md](./docs/DESIGN.md). This project started from the
take-home brief in [ASSIGNMENT.md](./ASSIGNMENT.md); this README
documents the resulting service, not the assignment process.

## Requirements

- Go 1.24+
- Docker with the Compose v2 plugin (`docker compose`, not the
  deprecated standalone `docker-compose` binary)

## Quick start

```bash
cp .env.example .env
make run
```

`make run` starts Postgres + Redis via Docker Compose, applies pending
migrations automatically, and starts the server on `:8080`.

```bash
make seed   # inserts demo wallets — see "Manual testing" below
curl -X POST localhost:8080/transfers/init
```

## API

### `POST /transfers/init`
Mints a fresh idempotency key. Nothing is persisted.

```json
{ "transactionId": "123456789012345678" }
```

`transactionId` is always a JSON **string** — it's a 63-bit Snowflake ID,
which routinely exceeds what JavaScript's `Number` can represent exactly.

### `POST /transfers`

```json
{
  "idempotencyKey": "<transactionId from init>",
  "fromWalletId": "<uuid>",
  "toWalletId": "<uuid>",
  "amount": 10.00
}
```

| Status | Meaning |
|---|---|
| `200` | Transfer processed |
| `422` | Domain failure — `SAME_WALLET`, `INVALID_AMOUNT`, `WALLET_NOT_FOUND`, or `INSUFFICIENT_BALANCE` |
| `409` | `idempotencyKey` reused with a different `fromWalletId`/`toWalletId`/`amount` |
| `400` | Malformed request body |

### `GET /transfers/{id}`
Always `200` if the transfer exists, `404` otherwise. The body's
`status` field (`PROCESSED` \| `FAILED`) carries the real outcome — HTTP
status here reflects whether the *lookup* succeeded, not the transfer.

### `GET /health`
`200` if both Postgres and Redis are reachable, `503` otherwise, with
per-dependency status in the body.

## Configuration

Set via environment variables (see `.env.example`):

| Variable | Purpose |
|---|---|
| `WORKER_ID` | Snowflake generator worker ID (0–1023) |
| `POSTGRES_DSN` | Postgres connection string |
| `REDIS_ADDR` | Redis address |
| `PORT` | HTTP port |
| `LOCK_TIMEOUT` | Postgres `lock_timeout` applied per transfer transaction |

## Testing

```bash
make test              # unit tests — fast, no Docker required
make test-integration  # + real Postgres/Redis via docker compose, run with -race
make lint               # golangci-lint
make fmt-check          # gofmt
```

`make test-integration` includes the two tests that matter most for a
service like this: a concurrency stress test (N concurrent transfers on
the same wallet pair land on the exact right final balance) and a
double-click race test (two near-simultaneous requests with the same
idempotency key produce exactly one transfer and identical responses).

## Project layout

```
cmd/server           wiring only
internal/domain      pure business types, no infra imports
internal/platform    generic infra bootstrap (postgres/redis/migrate/logger)
internal/config      env struct binding
internal/repository  persistence only, no business rules
internal/service     orchestration — the idempotency/locking flow lives here
internal/middleware  cross-cutting concerns, attached per route-group
internal/handler     thin HTTP layer + router
internal/idgen       Snowflake ID generator
migrations/          paired .up.sql/.down.sql, applied automatically at startup
scripts/             manual/demo fixtures — never used by automated tests
```

See [docs/DESIGN.md](./docs/DESIGN.md) §8 for the full layering rules.

## Manual testing / demo wallets

`make seed` inserts a fixed set of wallets against whatever Postgres
`POSTGRES_DSN` points at (defaults to the one `docker compose up -d
postgres` starts). Re-running it is safe — it never resets a wallet's
balance if the row already exists. Fixtures live in `scripts/seed.sql`,
which is deliberately *not* a golang-migrate migration (schema migrations
stay data-free); this is demo/manual-testing data only.

| Wallet | ID | Starting balance |
|---|---|---|
| Alice | `018f0000-0000-7000-8000-000000000001` | 1000.00 |
| Bob | `018f0000-0000-7000-8000-000000000002` | 500.00 |
| Carol | `018f0000-0000-7000-8000-000000000003` | 0.00 (for `INSUFFICIENT_BALANCE` demos) |

Example walkthrough against a running server (`make run`, then in
another shell):

```bash
# 1. Mint an idempotency key
curl -s -X POST localhost:8080/transfers/init
# → {"transactionId":"<snowflake>"}

# 2. Execute the transfer (Alice -> Bob, 10.00)
curl -s -X POST localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{
    "idempotencyKey": "<transactionId from step 1>",
    "fromWalletId": "018f0000-0000-7000-8000-000000000001",
    "toWalletId": "018f0000-0000-7000-8000-000000000002",
    "amount": 10.00
  }'
# → 200 {"id":"...","status":"PROCESSED",...}

# 3. Fetch it back
curl -s localhost:8080/transfers/<id from step 2>
# → 200 {"id":"...","status":"PROCESSED",...}
```
