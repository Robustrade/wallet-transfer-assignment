# Wallet Transfer Service

A small, reliable wallet-to-wallet transfer service in Go + PostgreSQL,
demonstrating idempotent request handling, double-entry ledger recording,
correct balances under concurrency, and safe state transitions.

The full design rationale is in [`DESIGN.md`](./DESIGN.md).

## Layout

```
cmd/server/            entrypoint: config, pool, migrate, HTTP server
internal/domain/       entities, statuses, money type, typed errors (no DB/HTTP)
internal/repository/   SQL only; every method takes an open transaction
internal/service/      transfer workflow: tx boundary, idempotency, locking
internal/handler/      HTTP transport: validation + error mapping
internal/migrate/      embedded schema + applier  (schema in internal/migrate/sql)
```

The **service owns the transaction boundary**; repository methods run inside it.
The whole transfer commits or rolls back as one unit.

## Run

```bash
go mod tidy        # resolve dependencies (needs network once)
make db-up         # start a Postgres 16 container on :5432 (Docker required)
make migrate       # apply schema + seed data, then exit
make run           # start the HTTP server on :8080
```

`make migrate` applies the embedded SQL files in `internal/migrate/sql/` and
exits immediately — you don't need to start the server just to set up the schema.
`make run` also applies migrations on startup, so it is safe to skip `migrate`
if you run the server right away.

The seed file (`0002_seed.sql`) inserts four demo wallets so you can transfer
right away:

| wallet    | balance (minor units) |
|-----------|-----------------------|
| wallet_1  | 100000                |
| wallet_2  | 50000                 |
| wallet_3  | 25000                 |
| wallet_4  | 0                     |

```bash
curl -i localhost:8080/transfers -H 'content-type: application/json' -d '{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}'
# → 201 {"transferId":"...","status":"PROCESSED",...}
```

---

## Manual testing

Each scenario below maps to a test in
[`internal/service/transfer_service_test.go`](internal/service/transfer_service_test.go).
Run `make run` first, then paste the commands.

### 1 — Happy path (`TestHappyPath`)

```bash
curl -i localhost:8080/transfers -H 'content-type: application/json' -d '{
  "idempotencyKey": "hp-1",
  "fromWalletId":  "wallet_1",
  "toWalletId":    "wallet_2",
  "amount":        500
}'
# → 201  status=PROCESSED
```

Verify the balance moved in Postgres:

```bash
psql postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable \
  -c "SELECT id, balance FROM wallets WHERE id IN ('wallet_1','wallet_2');"
# wallet_1: 99500   wallet_2: 50500
```

---

### 2 — Idempotent replay (`TestIdempotentReplay`)

Send the **exact same request twice**. The second call must return the identical
body and HTTP status without moving money again.

```bash
BODY='{"idempotencyKey":"idem-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":200}'

curl -s localhost:8080/transfers -H 'content-type: application/json' -d "$BODY"
# first call  → 201  (money moves)

curl -s localhost:8080/transfers -H 'content-type: application/json' -d "$BODY"
# second call → 201  (identical body, no second debit)
```

Check that `wallet_1` lost exactly 200 — not 400:

```bash
psql postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable \
  -c "SELECT id, balance FROM wallets WHERE id = 'wallet_1';"
```

---

### 3 — Idempotency conflict (`TestIdempotencyConflict`)

Reuse the same key with a **different amount**. Must be rejected.

```bash
curl -i localhost:8080/transfers -H 'content-type: application/json' -d \
  '{"idempotencyKey":"conflict-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
# → 201

curl -i localhost:8080/transfers -H 'content-type: application/json' -d \
  '{"idempotencyKey":"conflict-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":999}'
# → 422  {"error":"idempotency key reused with a different payload"}
```

---

### 4 — Insufficient funds recorded as FAILED (`TestInsufficientFundsRecordsFailed`)

`wallet_4` has 0 balance. The transfer is recorded as `FAILED` (not an error),
and a duplicate replays the same 422.

```bash
curl -i localhost:8080/transfers -H 'content-type: application/json' -d \
  '{"idempotencyKey":"broke-1","fromWalletId":"wallet_4","toWalletId":"wallet_1","amount":100}'
# → 422  status=FAILED

# Replay with the same key — still 422, no money moved
curl -i localhost:8080/transfers -H 'content-type: application/json' -d \
  '{"idempotencyKey":"broke-1","fromWalletId":"wallet_4","toWalletId":"wallet_1","amount":100}'
# → 422  (replayed, identical body)
```

---

### 5 — Concurrent double-spend (`TestConcurrencyNoDoubleSpend`)

Fire 10 parallel transfers of 10 000 each against `wallet_3` (balance 25 000).
Only 2 can succeed; the rest must record `FAILED`.

```bash
for i in $(seq 1 10); do
  curl -s localhost:8080/transfers -H 'content-type: application/json' -d \
    "{\"idempotencyKey\":\"cc-$i\",\"fromWalletId\":\"wallet_3\",\"toWalletId\":\"wallet_4\",\"amount\":10000}" &
done
wait

# Check final balance — must be 5000 (25000 - 2×10000), never negative
psql postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable \
  -c "SELECT id, balance FROM wallets WHERE id IN ('wallet_3','wallet_4');"
# wallet_3: 5000   wallet_4: 20000

# Verify exactly 2 PROCESSED and 8 FAILED
psql postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable \
  -c "SELECT status, count(*) FROM transfers GROUP BY status;"
```

The `FOR UPDATE` row-lock in sorted wallet order (see
[`repository.go:LockWallets`](internal/repository/repository.go)) serializes the
concurrent debits so no double-spend is possible and the balance never goes
negative — matching exactly what `TestConcurrencyNoDoubleSpend` asserts.

---

## Test

Tests run against a real Postgres started automatically via testcontainers
(Docker required):

```bash
make test          # go test ./... -race
```

Or point at an existing database and skip the container:

```bash
WALLET_TEST_DATABASE_URL=postgres://user:pass@localhost:5432/wallet?sslmode=disable \
  go test ./... -race
```

Covered: happy path, idempotent replay, idempotency-key conflict, insufficient
funds recorded as FAILED, wallet-not-found, ledger reconciliation, and a
concurrent double-spend test that drains a single hot wallet and asserts exactly
the right number of transfers succeed with no negative balance.

## API

`POST /transfers`

Request:

```json
{ "idempotencyKey": "abc123", "fromWalletId": "wallet_1", "toWalletId": "wallet_2", "amount": 100 }
```

`amount` is an integer in minor units (e.g. cents).

| Outcome | HTTP |
|---|---|
| Success (`PROCESSED`) | 201 |
| Duplicate (same key + body) | replays original status/body |
| Insufficient funds (`FAILED`, recorded) | 422 |
| Idempotency key reused with different body | 422 |
| Bad input / self-transfer / non-positive amount | 400 |
| Unknown wallet | 404 |
| Infrastructure error (rolled back) | 500 |

---

## PR Description (copy into the pull request)

**Schema.** `wallets`, `transfers`, `ledger_entries`, `idempotency_records`. Migrations seed four demo wallets (`wallet_1`..`wallet_4`).
Money is `BIGINT` minor units. Invariants are enforced in the database as a
backstop: `CHECK (balance >= 0)` (no overdraft), `CHECK (amount > 0)`,
`CHECK (from_wallet_id <> to_wallet_id)` (no self-transfer), foreign keys, and a
deferred constraint trigger asserting each transfer's ledger entries net to zero.

**Idempotency.** INSERT-first against the `idempotency_records` primary key
(not check-then-act). A concurrent duplicate blocks on the unique index until the
original commits, then reads the stored `COMPLETED` record and replays the
original response. The request payload is hashed; reusing a key with a different
body returns 422. Business outcomes (including `FAILED`) commit with their
idempotency record and are replayed; only infrastructure errors roll back and
leave the key free.

**Concurrency.** One transaction at `READ COMMITTED`. Both wallets are locked
with `SELECT ... FOR UPDATE` in sorted id order (deterministic ordering prevents
the A↔B deadlock). Row locks serialize concurrent debits on a hot wallet, so no
double-spend is possible. The funds check uses the locked balance; the DB
`CHECK` is a backstop.

**State machine.** A transfer is inserted `PENDING` and moves to `PROCESSED` or
`FAILED` in the same transaction via a guarded `UPDATE ... WHERE status='PENDING'`.
A `FAILED` transfer moves no money and has zero ledger entries.

**Assumptions / tradeoffs.** Wallets pre-seeded; single currency; synchronous
(no queue); pessimistic locking chosen over optimistic for clarity and hot-wallet
contention. See `DESIGN.md` for the alternatives considered.

**AI usage.** _Tool: ___. Used for ___ (design discussion / schema review / test
scaffolding). Transcript / prompts: ___._
