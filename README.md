# Wallet Transfer Assignment Repository

This repository is a reusable coding assignment template for evaluating backend engineers on wallet transfers, idempotency, concurrency control, and double-entry ledger design.

## Included

- `ASSIGNMENT.md` - candidate-facing prompt
- `.github/pull_request_template.md` - required PR structure
- `.github/workflows/ci.yml` - lint, format, test placeholder workflow
- `.github/workflows/sonarqube.yml` - SonarQube pull request analysis
- `.github/copilot-instructions.md` - repository-level Copilot review guidance
- `evaluation_guide.md` - reviewer rubric
- `branch-protection-checklist.md` - GitHub setup checklist

## Intended use

1. Mark this repository as a GitHub template repository.
2. Create one private repository per candidate from the template.
3. Add the candidate as a collaborator.
4. Ask them to submit via a pull request into `main`.
5. Enable required checks, SonarQube, and Copilot review in GitHub.

## Notes

- Copilot automatic pull request review is configured in GitHub repository or organization settings, not purely through files in the repo.
- The `copilot-instructions.md` file included here provides repository-specific review guidance once Copilot review is enabled.
- The CI workflow is language-agnostic by default and expects you to set the `LINT_CMD`, `FORMAT_CHECK_CMD`, and `TEST_CMD` repository variables or replace the commands directly.

## How to Submit Assignment

1. **Fork this repository** to your own GitHub account.
2. Complete the assignment described in [`ASSIGNMENT.md`](./ASSIGNMENT.md).
3. **Raise a Pull Request** back to this repository (`main` branch) with your full solution.

Your PR branch should be named: `solution/<your-name>` (e.g., `solution/jane-doe`).

# PR README

```text
HTTP handler  ->  service (orchestration)  ->  repository (Postgres)  ->  database
```

- **Domain models** (`services/transfer/internal/data/model`) carry the
  state-machine logic for `Transfer` and the `Wallet` / `LedgerEntry` types.
- **Service** (`services/transfer/internal/svc/impls`) orchestrates idempotency,
  locking, validation, ledger posting, and balance updates inside a single
  transaction.
- **Repositories** (`services/transfer/internal/repo/postgres`) own SQL only.
- **Handler** (`services/transfer/internal/handler`) decodes JSON, calls the
  service, and maps results to HTTP responses. No business logic.

Money is stored as `int64` minor units (paisa). Floating-point arithmetic is
never used.

### Directory layout

```text
cmd/server/                                       boot, DI wiring, graceful shutdown
services/transfer/init                             service initializer
services/transfer/route                            HTTP route registration
services/transfer/internal/svc/{ifaces,impls}      service interface + implementation
services/transfer/internal/repo/{iface,postgres}   repo interfaces + Postgres impls
services/transfer/internal/data/{model,request,response}  domain types and DTOs
services/transfer/internal/handler                 HTTP handler
common/util/money                                  Money type
common/error                                       AppError + sentinel errors
common/middleware                                  request_id, recovery, logging, error_writer
config/{init,infra}                                env-based config + infra clients
migrations                                         SQL migrations
tests/integration                                  integration & concurrency tests
```

---

## Schema

```sql
CREATE TABLE wallets (
    id            VARCHAR(64)  PRIMARY KEY,
    balance_minor BIGINT       NOT NULL DEFAULT 0 CHECK (balance_minor >= 0),
    currency      VARCHAR(3)   NOT NULL CHECK (char_length(currency) = 3),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE transfers (
    id              UUID         PRIMARY KEY,
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    request_hash    VARCHAR(64)  NOT NULL CHECK (char_length(request_hash) = 64),
    from_wallet_id  VARCHAR(64)  NOT NULL REFERENCES wallets(id),
    to_wallet_id    VARCHAR(64)  NOT NULL REFERENCES wallets(id),
    amount_minor    BIGINT       NOT NULL CHECK (amount_minor > 0),
    currency        VARCHAR(3)   NOT NULL CHECK (char_length(currency) = 3 OR currency = ''),
    status          VARCHAR(16)  NOT NULL CHECK (status IN ('PENDING','PROCESSED','FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE ledger_entries (
    id           BIGSERIAL    PRIMARY KEY,
    transfer_id  UUID         NOT NULL REFERENCES transfers(id),
    wallet_id    VARCHAR(64)  NOT NULL REFERENCES wallets(id),
    entry_type   VARCHAR(8)   NOT NULL CHECK (entry_type IN ('DEBIT','CREDIT')),
    amount_minor BIGINT       NOT NULL CHECK (amount_minor > 0),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (transfer_id, entry_type)
);

CREATE INDEX idx_ledger_wallet_created ON ledger_entries(wallet_id, created_at DESC);
CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet   ON transfers(to_wallet_id);
```

Three tables defined under `migrations/`. Constraints encode invariants
directly: `balance_minor >= 0`, `amount_minor > 0`, `from_wallet_id <>
to_wallet_id`, `UNIQUE (transfer_id, entry_type)` (exactly one DEBIT + one
CREDIT per transfer), `UNIQUE (idempotency_key)` (the dedup record).

---

## Idempotency strategy

Inside the transfer transaction:

```sql
INSERT INTO transfers (..., status) VALUES (..., 'PENDING')
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING ...;
```

If the insert returns a row → fresh transfer. If not → the unique-index xmax
lock blocks the follow-up `SELECT` until the original tx commits, after which
we read the committed outcome and return `replayed: true` ("block + return
original", preferred over a mid-flight `429 Try Again`).

All side effects (transfer row, wallet locks, ledger inserts, balance update,
outcome write) live in **one transaction** — a rollback releases the
unique-key claim, leaving no orphan idempotency rows. Same key + different
`request_hash` returns `409 IDEMPOTENCY_CONFLICT`, surfacing a buggy client
instead of silently replaying a different request. Insufficient funds /
currency mismatch are committed as `FAILED` so retries replay the failure
rather than re-attempting a doomed transfer.

---

## Concurrency strategy

`READ COMMITTED + SELECT ... FOR NO KEY UPDATE`, with wallet IDs sorted
lexicographically before the lock — deterministic acquisition order means any
two transfers touching the same pair acquire locks in the same order.
`FOR NO KEY UPDATE` (not `FOR UPDATE`) is the correct mode: the preceding
`INSERT INTO transfers` FK check already holds `FOR KEY SHARE` on both wallet
rows in the same tx, and `FOR UPDATE` would conflict with `FOR KEY SHARE` and
deadlock on `40P01` under concurrency; `FOR NO KEY UPDATE` is mutually
exclusive with itself (so concurrent debits still serialize) without
conflicting with `FOR KEY SHARE`. A single round-trip locks both rows, and
the balance update is one `UPDATE wallets SET balance_minor =
balance_minor + CASE id WHEN ... END`. `SERIALIZABLE` is deliberately avoided
to skip the `40001` retry loop on a critical section that is already
deadlock-free.

---

## State machine

`PENDING → PROCESSED | FAILED`. `PENDING` is transient mid-transaction and
never observed in committed state under normal operation. Business-outcome
failures (insufficient funds, currency mismatch) commit as `FAILED` so
duplicate retries replay the same outcome rather than re-attempting a doomed
transfer.

---

## API contract

The service exposes one write endpoint (`POST /transfers`) and one health
probe (`GET /healthz`). All bodies are `application/json`. There is no
authentication in v1 — production deployments should sit behind a gateway
or mTLS that enforces ownership of `fromWalletId`.

### `POST /transfers`

Submit a wallet-to-wallet transfer. Idempotent on `idempotencyKey`.

#### Request

| Header | Required | Value |
|---|---|---|
| `Content-Type` | yes | `application/json` |
| `X-Request-ID` | optional | Echoed back on the response. If absent, the server generates a UUID v4 and returns it. |

Request body schema:

| Field | Type | Required | Constraint | Notes |
|---|---|---|---|---|
| `idempotencyKey` | string | yes | length 1–255 | Caller-chosen unique key per logical transfer attempt. Re-using it with the same body returns the original outcome; re-using it with a different body returns `409 IDEMPOTENCY_CONFLICT`. |
| `fromWalletId` | string | yes | length 1–64, no leading/trailing whitespace | Source wallet ID. Must differ from `toWalletId` and the wallet must exist. Whitespace is rejected (not silently trimmed) so client-side marshalling bugs surface as `400 INVALID_WALLET_ID` instead of `404 WALLET_NOT_FOUND`. |
| `toWalletId` | string | yes | length 1–64, no leading/trailing whitespace | Destination wallet ID. Same rules as `fromWalletId`. |
| `amount` | integer | yes | `> 0` | Amount in **minor units** (paisa for INR; cents for USD). E.g. `100` means INR 1.00. JSON number — never a string, never a float. |

Body limits: ≤ 64 KiB total; **unknown fields are rejected** (`BAD_JSON`).

Currency is **not** in the request — it is taken from the wallets. Both
wallets must share the same currency (otherwise the transfer is committed
as `FAILED` with reason `CURRENCY_MISMATCH`).

Example:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId":   "wallet_1",
  "toWalletId":     "wallet_2",
  "amount":         100
}
```

#### Responses

| Status | Meaning |
|---|---|
| `201 Created` | Fresh transfer was processed and committed |
| `200 OK` | Idempotent replay (same key + same body) — original outcome returned. `replayed=true` |
| `400 Bad Request` | Validation error — see error code table below |
| `404 Not Found` | `fromWalletId` and/or `toWalletId` does not exist |
| `409 Conflict` | Same idempotency key reused with a **different** body (`IDEMPOTENCY_CONFLICT`) |
| `413 Payload Too Large` | Request body exceeded the 64 KiB cap |
| `422 Unprocessable Entity` | Fresh business-outcome failure — `INSUFFICIENT_FUNDS` or `CURRENCY_MISMATCH`. The transfer row is **committed** as `FAILED`. A retry with the same key returns the same outcome at `200 OK` with `replayed=true` (not `422`). |
| `5xx` | Internal server error. Safe to retry with the same `idempotencyKey`. |

All responses include the header `X-Request-ID` (echoed from the request or
generated). Use it to correlate with server logs.

##### Success body (`201` fresh, `200` replay)

```json
{
  "id":             "550e8400-e29b-41d4-a716-446655440000",
  "idempotencyKey": "abc123",
  "fromWalletId":   "wallet_1",
  "toWalletId":     "wallet_2",
  "amount":         100,
  "currency":       "INR",
  "status":         "PROCESSED",
  "createdAt":      "2026-04-28T12:34:56Z",
  "updatedAt":      "2026-04-28T12:34:56Z",
  "replayed":       false
}
```

(`failureReason` is omitted from the body when `status=PROCESSED`.)

Field reference:

| Field | Type | Description |
|---|---|---|
| `id` | string (UUID v4) | Server-assigned transfer identifier. Stable across replays. |
| `idempotencyKey` | string | Echo of the request key. |
| `fromWalletId`, `toWalletId` | string | Echo of the request wallets. |
| `amount` | integer | Echo of the request amount (minor units). |
| `currency` | string (ISO-4217, 3 chars) | Currency of the participating wallets. |
| `status` | enum | `PROCESSED` or `FAILED`. (`PENDING` is mid-transaction only and never appears in responses.) |
| `failureReason` | string | Present only when `status = FAILED`; one of `INSUFFICIENT_FUNDS`, `CURRENCY_MISMATCH`. Omitted otherwise. |
| `createdAt`, `updatedAt` | string (RFC 3339, UTC) | Row timestamps. |
| `replayed` | boolean | `true` on any `200` replay; `false` on a fresh `201` or fresh `422`. |

##### Failed body (`422` fresh, `200` replay)

Same shape as success, with `"status": "FAILED"` and a populated
`failureReason`:

```json
{
  "id":             "0c3fe1ad-2c8f-4c4e-bd8b-9b1e93d2a217",
  "idempotencyKey": "abc124",
  "fromWalletId":   "wallet_2",
  "toWalletId":     "wallet_1",
  "amount":         99999,
  "currency":       "INR",
  "status":         "FAILED",
  "failureReason":  "INSUFFICIENT_FUNDS",
  "createdAt":      "2026-04-28T12:34:57Z",
  "updatedAt":      "2026-04-28T12:34:57Z",
  "replayed":       false
}
```

##### Error body (`4xx` / `5xx`)

```json
{
  "code":       "IDEMPOTENCY_CONFLICT",
  "message":    "idempotency key reused with a different request body",
  "request_id": "8f4e2c10-7a91-4f8d-9b3e-c5d2e1a0b8f4"
}
```

| Field | Type | Description |
|---|---|---|
| `code` | string | Stable machine-readable error code (see table below). Match on this, not on `message`. |
| `message` | string | Human-readable description. May change between releases — do not parse. |
| `request_id` | string (UUID v4) | The request id (matches `X-Request-ID` header). Quote this when reporting issues. |

Error code reference (returned in the error-body `code` field):

| HTTP | `code` | When |
|---|---|---|
| 400 | `BAD_JSON` | Malformed JSON or unknown field in the body |
| 400 | `INVALID_IDEMPOTENCY_KEY` | Empty / missing / > 255 chars |
| 400 | `INVALID_WALLET_ID` | Empty / missing / > 64 chars / contains leading or trailing whitespace |
| 400 | `INVALID_AMOUNT` | Missing / ≤ 0 / not an integer |
| 400 | `SAME_WALLET` | `fromWalletId == toWalletId` |
| 404 | `NOT_FOUND` | Route does not exist (e.g. wrong path) |
| 404 | `WALLET_NOT_FOUND` | Route matched but `fromWalletId` and/or `toWalletId` does not exist |
| 405 | `METHOD_NOT_ALLOWED` | Route exists but the HTTP method isn't supported (e.g. `GET /transfers`) |
| 409 | `IDEMPOTENCY_CONFLICT` | Same `idempotencyKey` previously used with a different body |
| 413 | `PAYLOAD_TOO_LARGE` | Request body > 64 KiB |
| 5xx | `INTERNAL_ERROR` | Unexpected server error — safe to retry with the same `idempotencyKey` |

Note: a `422` business-outcome failure (`INSUFFICIENT_FUNDS` or
`CURRENCY_MISMATCH`) is returned as a **transfer-shaped body** (status=FAILED,
`failureReason` populated), not an error-shaped body. Retries with the same
key return the same `422` payload.

#### Idempotency semantics

- `idempotencyKey` is unique across the lifetime of the system; there is no
  expiry. Pick fresh keys per logical attempt — usually a UUID generated by
  the client.
- For a given key, the server stores a `request_hash =
  SHA-256(<fromWalletId> | <toWalletId> | <amountMinor>)`. Same key +
  matching hash → original outcome is replayed. Same key + different hash
  → `409 IDEMPOTENCY_CONFLICT`.
- On a `5xx` response or any transport error, retry with the same
  `idempotencyKey` and the **same** body. The server's unique-index xmax lock
  blocks concurrent retries until the original transaction commits, so the
  retry sees a committed outcome (`PROCESSED` or `FAILED`).
- A `422 FAILED` is itself a committed outcome. Retries return that same
  `FAILED` body at `200 OK` with `replayed=true`; they do not re-attempt the
  transfer.

#### Server-side effects

| Outcome | Effects |
|---|---|
| `201 PROCESSED` | 1 `transfers` row (`status=PROCESSED`), 2 `ledger_entries` (1 DEBIT + 1 CREDIT) for the same amount, both `wallets.balance_minor` updated atomically in one `UPDATE … CASE WHEN`. |
| `200 PROCESSED` (replay) | None. Returns the committed row from the original request. |
| `422 FAILED` | 1 `transfers` row (`status=FAILED`, `failure_reason` set). No ledger entries; balances unchanged. |
| `200 FAILED` (replay) | None. Returns the committed `FAILED` row. |
| `409 IDEMPOTENCY_CONFLICT` | None. Original row stands. |
| `4xx` validation / `5xx` | None. Validation errors are rejected before any DB write; system errors roll back the open transaction. |

### `GET /healthz`

Liveness/readiness probe. No request body or parameters.

| Status | Body | Meaning |
|---|---|---|
| `200 OK` | `{"status":"ok"}` | DB pool ping succeeded within 1 s. |
| `503 Service Unavailable` | `{"status":"down","reason":"database not reachable"}` | DB ping failed or timed out. |

---

## Observability

The service emits structured logs via `log/slog` configured by `LOG_JSON` (default `true`) and `LOG_LEVEL` (default `info`). Notable events:

| Event | Level | Fields |
|---|---|---|
| Per-request access log | `info` | `method`, `path`, `status`, `duration_ms`, `request_id` |
| Idempotent replay | `info` | `idempotency_key`, `transfer_id`, `original_status` |
| Idempotency conflict | `warn` | `idempotency_key`, `existing_transfer_id` |
| Transfer failed | `warn` | `transfer_id`, `idempotency_key`, `reason`, `from_wallet_id`, `to_wallet_id`, `amount_minor` |
| Panic recovered | `error` | `panic`, `stack`, `request_id` |
| Server lifecycle | `info` | `addr` on start, plain message on shutdown |

Every inbound request is tagged with an `X-Request-ID` header (generated by the `RequestID` middleware if not supplied by the caller) and the same value is threaded through `slog` so a single request's log lines can be grep'd from a JSON log stream.

Out of scope for v1: Prometheus metrics, OpenTelemetry tracing, audit-trail exports. The ledger itself is the audit trail; metrics and traces would be the next addition for production rollout.

---

## How to run

### Prerequisites

- Go 1.22+ (CI runs 1.24)
- Docker + Docker Compose (for the local Postgres)
- [`golang-migrate`](https://github.com/golang-migrate/migrate/tree/master/cmd/migrate) CLI for `make migrate-{up,down}`
  ```bash
  brew install golang-migrate          # macOS
  ```
- `psql` (for `make seed`; ships with `postgresql-client` on most distros)
- Optional: `golangci-lint` v2.5.0+ for `make lint` locally
  ```bash
  brew install golangci-lint           # macOS
  ```

Copy `.env.example` to `.env` if you want to override any of the defaults
the `Makefile` already sets.

### Bring up the service

```bash
make pg-up && make migrate-up && make seed   # seeds wallet_1=10000, wallet_2=0, INR
make run                                      # PORT=8080
```

Exercise the API (substitute the body to see each branch):

```bash
URL=localhost:8080/transfers
H='Content-Type: application/json'

# Fresh PROCESSED (201)
curl -sS -X POST $URL -H "$H" -d '{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
# Replay same body (200, replayed=true)
curl -sS -X POST $URL -H "$H" -d '{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
# Insufficient funds → 422 FAILED
curl -sS -X POST $URL -H "$H" -d '{"idempotencyKey":"k2","fromWalletId":"wallet_2","toWalletId":"wallet_1","amount":99999}'
# Self-transfer → 400 SAME_WALLET
curl -sS -X POST $URL -H "$H" -d '{"idempotencyKey":"k3","fromWalletId":"wallet_1","toWalletId":"wallet_1","amount":1}'
# Same key, different body → 409 IDEMPOTENCY_CONFLICT
curl -sS -X POST $URL -H "$H" -d '{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":999}'
```

Full stack via Docker Compose: `make compose-up && make migrate-up && make seed`
(app on `:8080`); `make compose-down` to tear down.

---

## How to test

```bash
# Unit tests with the race detector (no DB needed).
make test

# Integration + concurrency tests (requires a running Postgres).
# Note: for assignment scope, the integration tests default to the SAME
# Postgres instance used by `make run` / `make seed`. Each test TRUNCATEs the
# tables in setUp, so running `make test-int` will wipe any interactively
# seeded rows. Override INTEGRATION_DATABASE_URL to use a separate database.
make pg-up
make migrate-up
INTEGRATION_DATABASE_URL=postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable \
  make test-int

# Lint + format check.
make lint
make fmt-check
```

The unit tests cover the money type, the transfer state machine, and the
service orchestration with hand-written fakes (S1-S9).

The integration tests (`-tags=integration`) cover:

| ID  | What it asserts                                                                  |
|-----|----------------------------------------------------------------------------------|
| I1  | Happy path over HTTP - 201, balances and ledger correct                          |
| I2  | Idempotent replay - 200, replayed=true, identical id, balances unchanged         |
| I3  | Ledger zero-sum (`SUM(DEBIT) == SUM(CREDIT)`) + per-wallet `balance_minor == initial + ledger net` after a 30-transfer ring across 5 wallets |
| I4  | Insufficient funds - 422 FAILED; replay returns the same FAILED                  |
| I5  | Same key, different body - 409 IDEMPOTENCY_CONFLICT, original transfer intact    |

Concurrency tests:

| ID  | What it asserts                                                                  |
|-----|----------------------------------------------------------------------------------|
| C1  | 50 goroutines debit one wallet, exactly 30 succeed and 20 are FAILED             |
| C2  | 100 goroutines submit the same idempotency key, exactly 1 transfer + 2 ledger    |
| C3  | Cross-wallet bidirectional load, no deadlocks, balances net to zero change       |
| C4  | 80-goroutine mixed load (40 same-key replays + 40 unique transfers); ledger zero-sum and `balance_minor == initial + ledger net` invariants hold |

---

## Tradeoffs / Assumptions

- **`request_hash` is enforced** (strict body match): same key + different body returns `409`, surfacing client bugs instead of silently replaying.
- **No async worker / outbox / retry queue** — single-transaction atomicity makes per-side-effect tracking redundant, and `PENDING` is transient.
- **`idempotency_records` table collapsed** into `transfers.idempotency_key UNIQUE` — functionally equivalent, one less moving part.
- **No `ledger_updated` / `wallet_updated` per-side-effect flags** on the transfer row — single-transaction atomicity makes them redundant.
- **CQRS-lite:** `wallets.balance_minor` is the denormalized read view over the append-only `ledger_entries` log; integration test I3 verifies `SUM(ledger) == balance` per wallet.
- **Single Postgres ACID transaction** instead of TC/C / Saga / 2PC — distributed-transaction machinery would be cost without benefit at this scope.
- **READ COMMITTED + `FOR NO KEY UPDATE`** over `SERIALIZABLE` — the critical section is already deadlock-free under deterministic lock ordering; no `40001` retry loop.
- **JSON `amount` decoded as a number** per the assignment example. Production would prefer string to avoid JS float-precision risk past `2^53 - 1`.
- **Single currency per transfer**; mismatch commits as `FAILED` with reason `CURRENCY_MISMATCH`. No FX.
- **GORM as connection / transaction manager only** — every statement is hand-written parameterized SQL via `tx.Raw(...).Row().Scan(...)` or `tx.Exec(...)`. No ORM, AutoMigrate, hooks, or struct-tag scanning.
- **No authentication / authorization** — out of scope. Production would sit behind an API gateway or mTLS enforcing ownership of `fromWalletId`.

---

## AI usage disclosure

1. **Tool:** Claude Code (Anthropic's CLI for Claude).
2. **How I used it:** strict plan-then-execute. Claude explored the assignment repo and an internal Go service for conventions, drafted a plan covering schema / layering / concurrency / idempotency / tests, refined it against my own judgment, then implemented one package or test file at a time with `go test` and `go vet` after each layer. Each review pass (security, simplify) was its own iteration.
3. **Transcript:** every prompt captured in [`AI_TRANSCRIPT.md`](./AI_TRANSCRIPT.md), grouped by phase (planning / implementation / review).
