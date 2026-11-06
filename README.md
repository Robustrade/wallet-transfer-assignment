# Wallet Transfer Service

A wallet-to-wallet transfer API in Go and PostgreSQL. It provides **exactly-once semantics per
idempotency key**, a **double-entry ledger**, and **no double spend under concurrency**. Correctness
is enforced by the database where possible, not only by application code.

- Design note (contract, invariants, failure modes, tradeoffs): [`docs/DESIGN.md`](docs/DESIGN.md)
- Assignment brief: [`ASSIGNMENT.md`](ASSIGNMENT.md)

## Quick start

The only prerequisite is Docker with Compose v2. On macOS without Docker Desktop, Colima works:
`brew install colima docker docker-compose docker-buildx && colima start`.

```bash
docker compose up --build        # or: make up   (detached, waits until healthy)
```

That single command:

1. starts **Postgres 17** and waits for its health check
2. runs the **`migrate` job**: applies the schema (advisory-locked, recorded in `schema_migrations`)
   and seeds demo wallets. Re-running it is harmless
3. starts the **API on `http://localhost:8080`** once the migration has exited successfully, with a
   readiness health check

Seeded wallets (amounts in minor units): `wallet_1` = 10000, `wallet_2` = 5000, `wallet_3` = 0.

```bash
make demo     # scripted walkthrough of every guarantee; fails loudly if any check fails
make test     # full test suite (unit, integration, concurrency) in Docker with -race
make down     # stop (keeps data)       make reset   # stop and delete data
```

## Try it

```bash
curl -i -X POST localhost:8080/transfers -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
# → 201 Created. Repeat the exact command → 201 with "Idempotent-Replayed: true", no money moved.

curl -s localhost:8080/wallets/wallet_1
curl -s localhost:8080/transfers/<id>
```

### Postman

Import [`postman/wallet-transfer.postman_collection.json`](postman/wallet-transfer.postman_collection.json)
(Postman → Import). It has 21 requests in 6 folders, and every request has assertions:
- health
- wallets
- transfers and idempotency (including a byte-identical replay check)
- business failures
- validation
- cleanup

Use **Run collection** to check the whole API in one click. Every run uses fresh idempotency keys and
is balance-neutral. If the API runs on another port, change the `baseUrl` collection variable.

## API

| Method & path | Success | Errors |
|---|---|---|
| `POST /transfers` | `201` transfer `PROCESSED` | `422 INSUFFICIENT_FUNDS` (transfer recorded as `FAILED`), `422 IDEMPOTENCY_KEY_REUSED`, `400 VALIDATION_ERROR`, `404 WALLET_NOT_FOUND`, `413`, `503 SERVICE_BUSY` (retryable), `500` |
| `GET /transfers/{id}` | `200` transfer | `400`, `404 TRANSFER_NOT_FOUND` |
| `GET /wallets/{id}` | `200 {id, balance, updatedAt}` | `404 WALLET_NOT_FOUND` |
| `GET /healthz` / `GET /readyz` | liveness / database readiness | `503` when the database is unreachable |

- `idempotencyKey` is **required**.
- Amounts are **positive integers in minor units**. Floats, exponents and strings are rejected.
- Replays return the **original status code and body**, plus `Idempotent-Replayed: true`.
- Errors use one envelope: `{"error":{"code","message"}}`.
- Every response carries `X-Request-ID`, which also appears in the logs.

## How it works

```
HTTP handler  ── decode/shape-check, map errors to status codes       internal/handler/httpapi
   │
Service       ── transaction boundary, idempotency, lock order          internal/service
   │                │
Domain        ── state machine, funds rule, ledger pair (pure)          internal/domain
   │
Repository    ── SQL only, behind interfaces owned by the service       internal/repository/postgres
```

`POST /transfers` runs as **one** `READ COMMITTED` transaction:

1. **Claim the idempotency key** with `INSERT … ON CONFLICT DO NOTHING`.
   - A concurrent duplicate blocks on the primary-key index until the first request commits, then
     replays its result.
   - If the key already exists with a different payload (checked by SHA-256 fingerprint), the
     request is rejected.
2. **Lock both wallets** with `SELECT … FOR UPDATE`, always in ascending id order, so A→B and B→A
   transfers can't deadlock.
3. **Insert the transfer** as `PENDING`. The domain then decides `PROCESSED` or
   `FAILED(INSUFFICIENT_FUNDS)` from the locked balance.
4. **If processed**, update both balances and append the DEBIT and CREDIT ledger rows.
5. **Guarded status update**: `UPDATE … WHERE status = 'PENDING'`, which must affect exactly 1 row.
6. **Commit.** The key, transfer, ledger rows and balances become visible together. A committed key
   always points at a finished transfer.

The database enforces these guarantees itself:
- `CHECK (balance >= 0)`
- foreign keys on every ledger row
- `UNIQUE (transfer_id, type)`, so a transfer has at most one debit and one credit
- a status / failure-reason consistency `CHECK`
- one transfer per key, via a `DEFERRABLE` foreign key

Rationale, alternatives and failure modes are in [`docs/DESIGN.md`](docs/DESIGN.md). That includes
how each SOLID principle maps to the code (§12) and why there is no saga or workflow orchestration, plus
exactly when one would be introduced (§11).

## Testing

| Layer | What is proven | Needs Postgres |
|---|---|---|
| `internal/domain` | validation, state transitions, terminal-state guards, ledger pair | no |
| `internal/handler/httpapi` | strict decoding, error→status mapping, replay header, panic recovery, request ids | no |
| `migrations` | the DB itself rejects invalid states (negative balance, orphan ledger rows, double debit…) | yes |
| `internal/platform/migrate` | migrations apply once; concurrent runners are safe; re-seeding never resets balances | yes |
| `internal/repository/postgres` | rollback, guarded updates, key claims, lock timeout → `ErrBusy` | yes |
| `internal/service` | success, insufficient funds, replay, key reuse, unknown wallet, **ledger invariants after every scenario** | yes |
| `internal/service` (concurrency) | 50 racing debits never double spend; 20 racing duplicates execute once; A→B/B→A never deadlock; a random storm with duplicate deliveries conserves money | yes |
| `internal/service` (atomicity) | a crash injected after **every** write inside a transfer (and a panic) leaves no trace, and the same key then succeeds | yes |
| `internal/app` | end-to-end over HTTP with the real wiring | yes |

- `make test` runs everything in Docker against a throwaway Postgres on tmpfs. Each test gets its
  own database.
- Without Docker, run `TEST_DATABASE_URL=postgres://user@localhost:5432/postgres?sslmode=disable make test-local`.
- Without `TEST_DATABASE_URL`, the integration tests are **skipped with a message**, and the unit
  tests still run.
- The concurrency tests were checked by mutation:
  - Removing `FOR UPDATE` fails the double-spend test. The `CHECK (balance >= 0)` backstop rejects
    the overdraw.
  - Removing the lock ordering fails the deadlock test with `SQLSTATE 40P01`.

## Make targets

| Target | Description |
|---|---|
| `make up` / `down` / `reset` | start (and wait for healthy), stop, stop and wipe data |
| `make demo` | scripted walkthrough and smoke test against the running stack |
| `make test` | all tests in Docker (`-race -cover`) |
| `make lint` / `make fmt-check` | golangci-lint v1.64.8 / gofmt, in Docker |
| `make logs` / `make ps` | follow app logs / service status |
| `make build`, `test-local`, `lint-local`, `fmt-check-local` | same tasks with a local Go toolchain |

Configuration is read from environment variables. Defaults are documented in [`.env.example`](.env.example).

## CI

The workflow in `.github/workflows/ci.yml` runs `LINT_CMD`, `FORMAT_CHECK_CMD` and `TEST_CMD`, so
these repository variables need to be set:

```
LINT_CMD=golangci-lint run ./...
FORMAT_CHECK_CMD=test -z "$(gofmt -l .)"
TEST_CMD=go test ./... -race -cover          # unit tests; integration tests skip without a DB
# or, on a runner with Docker, the full suite:
TEST_CMD=docker compose --profile test run --rm test
```

`CGO_ENABLED=1` is needed for `-race`.

## Project layout

```
cmd/wallet/                    binary: serve | migrate [--seed] | healthcheck
internal/domain/               entities, state machine, business rules
internal/service/              transfer workflow, idempotency, persistence ports
internal/repository/postgres/  Postgres adapters (SQL only)
internal/handler/httpapi/      HTTP transport and middleware
internal/app/                  wiring
internal/platform/             config, logging, database, migrator
internal/testutil/pgtest/      isolated database per test
migrations/                    embedded schema and seed SQL (+ schema constraint tests)
scripts/demo.sh                API walkthrough
docs/DESIGN.md                 design note
```

---

## About this repository (assignment template)

This repository is a reusable coding assignment template for evaluating backend engineers on wallet
transfers, idempotency, concurrency control, and double-entry ledger design. It includes
`ASSIGNMENT.md`, a PR template, CI and SonarQube workflows, Copilot review guidance,
`evaluation_guide.md` and `branch-protection-checklist.md`.

### How to Submit Assignment

1. **Fork this repository** to your own GitHub account.
2. Complete the assignment described in [`ASSIGNMENT.md`](./ASSIGNMENT.md).
3. **Raise a Pull Request** back to this repository (`main` branch) with your full solution.

Your PR branch should be named: `solution/<your-name>` (e.g., `solution/jane-doe`).
