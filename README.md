# Wallet Transfer Service

A reliable wallet-to-wallet transfer service demonstrating safe handling of
**idempotency**, **concurrency**, **ledger consistency**, and **transfer state
transitions**.

> Submission for the Robustrade `wallet-transfer-assignment`.

---

## Quick start

The fastest way to run the service end-to-end is with Docker Compose. The
compose file builds the application image from source (multi-stage Dockerfile),
starts a Postgres 16 container, waits for it to be healthy, and then boots the
Spring Boot app.

```bash
docker compose up --build
```

That's it. When the logs settle the service is listening on
`http://localhost:8080`.

- **App** → `http://localhost:8080`
- **Postgres** → `localhost:5432` (`wallet` / `wallet` / `wallet`)

Flyway runs on startup and creates the schema. The migration also **seeds two
wallets** (`wallet_1` with balance `10000`, `wallet_2` with balance `0`) so the
sample requests below work immediately against a fresh stack.

To stop:

```bash
docker compose down            # stop containers, keep the DB volume
docker compose down -v         # stop containers AND wipe the DB volume
```

## API

### `POST /transfers`

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId":   "wallet_1",
  "toWalletId":     "wallet_2",
  "amount": 100
}
```

| Status | Condition |
|--------|-----------|
| 201 Created             | First successful execution — transfer `PROCESSED` |
| 200 OK                  | Idempotent replay — same key, same payload — original body returned |
| 422 Unprocessable Entity | First execution, insufficient funds (transfer persisted as `FAILED`, replayable) |
| 400 Bad Request          | Validation error or self-transfer |
| 404 Not Found            | Source or destination wallet does not exist |
| 409 Conflict             | Same idempotency key reused with a different payload |
| 500 Internal Server Error | Unexpected failure |

Successful body:

```json
{
  "transferId":   "33333333-3333-3333-3333-333333333333",
  "status":       "PROCESSED",
  "fromWalletId": "wallet_1",
  "toWalletId":   "wallet_2",
  "amount":       100,
  "createdAt":    "2026-05-21T10:15:30Z"
}
```

Error envelope:

```json
{ "error": "WALLET_NOT_FOUND", "message": "wallet not found: ..." }
```

### Sample walkthrough

The seeded wallets (`wallet_1` = 10000, `wallet_2` = 0) make these runnable as
soon as `docker compose up` is ready.

```bash
# 1. Happy path — first execution, returns 201
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'

# 2. Replay — same key, same payload — returns 200 with identical body
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'

# 3. Conflict — same key, different amount — returns 409
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":200}'

# 4. Insufficient funds — first time, returns 422 with FAILED status
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"k2","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":999999}'
```

---

## Design highlights

The full design rationale is in [`approach.md`](./approach.md). Condensed:

- **Single DB transaction per request** at `READ_COMMITTED`. The transfer row,
  both wallet updates, both ledger entries, and the idempotency record commit
  together or roll back together.
- **Pessimistic row locking** on wallets via `SELECT ... FOR UPDATE`
  (Hibernate `PESSIMISTIC_WRITE` on Postgres). Wallets are locked in ascending
  string id order so reverse-direction transfers acquire the same locks in
  the same order — no deadlocks.
- **Structural guarantees at the storage layer**, independent of application
  logic:
  - `CHECK (balance >= 0)` on `wallets` — cannot double-spend even with a
    buggy service.
  - `UNIQUE (transfer_id, type)` on `ledger_entries` — at most one DEBIT and
    at most one CREDIT row per transfer. The service writes both entries in
    the same DB transaction as the transfer's PROCESSED state transition, so
    every PROCESSED transfer has exactly one of each.
  - `CHECK (from_wallet_id <> to_wallet_id)` on `transfers`.
- **Idempotency** via a dedicated `idempotency_records` table that stores the
  request hash plus the cached response body. Replays return the cached body
  byte-for-byte with HTTP 200. A reused key with a different payload returns
  409.

---

## Running the tests

```bash
# Unit + service + entity tests (no Docker needed)
./gradlew test

# Integration + concurrency tests against a real Postgres
# (Testcontainers — requires a running Docker daemon)
./gradlew integrationTest

# Everything: tests + integration tests + Spotless (format) + Checkstyle (lint)
./gradlew check
```

### Code quality gates

| Gate | Tool | Config | Auto-fix |
|------|------|--------|----------|
| Format | Spotless | rules inline in `build.gradle.kts` | `./gradlew spotlessApply` |
| Lint   | Checkstyle 10.17 | `config/checkstyle/checkstyle.xml` | manual |

Both run as part of `./gradlew check` and any violation fails the build.


## See also

- [`ASSIGNMENT.md`](./ASSIGNMENT.md) — original prompt
- [`approach.md`](./approach.md) — design note (schema, idempotency, concurrency, tradeoffs)
- [`AI_TRANSCRIPT.md`](./AI_TRANSCRIPT.md) — disclosure of AI usage during the build
