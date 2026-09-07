# Wallet Transfer Service

A small HTTP service for wallet-to-wallet transfers with **API-level exactly-once
semantics**, a **double-entry ledger**, correct balances under concurrency, and a
safe transfer state machine.

The assignment prompt is in [`ASSIGNMENT.md`](./ASSIGNMENT.md); the design note
(contract, failure modes, idempotency and concurrency strategy) is in
[`docs/design.md`](./docs/design.md).

## Architecture

Four layers, dependencies pointing inward, interfaces owned by the consumer:

```
cmd/server                      process wiring, graceful shutdown
  └── internal/handler/http      transport: decode, validate, map errors  (thin)
        └── internal/service     workflow: idempotency, tx boundary, locking
              └── internal/domain entities, Money, transfer state machine (pure)
        ▲
        └── internal/repository  persistence (implements service ports)
              ├── postgres        pgx; SELECT ... FOR UPDATE; embedded migrations
              └── memory          in-process; snapshot/rollback; tests + REPO=memory
```

- `internal/domain` imports nothing else — trivially unit-testable.
- `internal/service` defines the persistence interfaces (`ports.go`); `postgres`
  and `memory` implement them. Only `cmd/server` sees concrete types.
- Handlers contain no business logic; repositories contain no workflow decisions.

## Requirements

- Go >= 1.24
- PostgreSQL 16 (Docker Compose file provided), or run with `REPO=memory`

## Run

### Against PostgreSQL

```bash
# 1. start Postgres (or point DATABASE_URL at your own)
make db-up

# 2. run the server — migrations are applied automatically on startup
export DATABASE_URL='postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable'
make run          # listens on :8080

# 3. (optional) load demo wallets wallet_1 / wallet_2 / wallet_3
make seed
```

### Without a database

```bash
make run-memory   # in-memory repository, seeded with demo wallets
```

## API

| Method & path | Purpose |
| --- | --- |
| `POST /transfers` | create a transfer (idempotent on `idempotencyKey`) |
| `GET /wallets/{id}/balance` | current wallet balance |
| `GET /healthz` / `GET /readyz` | liveness / readiness |

`amount` is a positive integer in **minor units** (e.g. cents).

```bash
curl -X POST localhost:8080/transfers -H 'content-type: application/json' -d '{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 2500
}'
```

Status codes: `201` created · `200` idempotent replay · `409` key reused with a
different payload · `422` insufficient funds (transfer stored as `FAILED`) ·
`404` unknown wallet · `400` malformed request.

## Test

```bash
make test              # unit + in-memory e2e, race detector, coverage → coverage/cover.out
make test-integration  # also runs //go:build integration tests against $TEST_DATABASE_URL
make fmt-check          # gofmt
make lint               # golangci-lint
```

- **Unit** (`internal/domain`, `internal/service`): state machine, Money math,
  idempotency replay/conflict, insufficient-funds path, concurrent-debit
  double-spend guard — all hermetic.
- **Handler** (`internal/handler/http`): status/body mapping with a mocked service.
- **e2e** (`test/e2e`): full HTTP stack over the in-memory repo, including 40
  concurrent debits of one wallet.
- **Integration** (`internal/repository/postgres`, tagged `integration`): real
  Postgres — `SELECT ... FOR UPDATE` serialisation, unique-constraint dedup,
  atomic ledger pair. Skipped unless `TEST_DATABASE_URL` is set.

## Schema

`wallets`, `transfers`, `idempotency_records`, `ledger_entries` —
[`internal/repository/migrations/0001_init.up.sql`](./internal/repository/migrations/0001_init.up.sql).
Correctness is enforced by the database:

- `transfers.idempotency_key` **UNIQUE** — creating the transfer row *is* the
  idempotency claim.
- `ledger_entries` **UNIQUE (transfer_id, type)** — at most one DEBIT and one
  CREDIT per transfer.
- `wallets.balance` **CHECK (>= 0)**, `transfers.state` **CHECK** in
  `{PENDING, PROCESSED, FAILED}`, no-self-transfer CHECK, foreign keys throughout.

## Concurrency & idempotency (short version)

- One database transaction per transfer. Both wallet rows are locked with
  `SELECT ... FOR UPDATE` in ascending id order (deadlock-free) **before** the
  transfer is inserted, to avoid FK lock upgrades.
- Serialization failures / deadlocks are retried a bounded number of times in the
  Postgres `TxManager`.
- A duplicate `idempotencyKey` is detected by the unique constraint; the original
  transfer is replayed with no new side effects. A key reused with a different
  request fingerprint (`request_hash`) returns `409`.
- Insufficient funds commits a `FAILED` transfer (no balance/ledger change) so a
  retry replays the failure instead of re-executing.

See [`docs/design.md`](./docs/design.md) for the full treatment.
