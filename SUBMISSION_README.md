# Wallet Transfer Service — Submission

This is a wallet-to-wallet transfer service built in Go. It handles concurrent transfers safely, keeps a double-entry ledger, and makes transfers idempotent.

---

## What you need before starting

- **Go 1.24+** — check with `go version`
- **Docker Desktop** — needs to be running (for Postgres and for the full test suite)

That's it. Everything else (migrations, seed data) runs automatically.

---

## Step 1 — Start Postgres

```bash
make db-up
```

This starts a Postgres 16 container on port 5432. The app will auto-create all tables and seed 3 test wallets when you run it.

---

## Step 2 — Start the server

Open a **second terminal** and run:

```bash
make run
```

You should see:

```
{"level":"INFO","msg":"http server listening","addr":":8080"}
```

The server is now running at `http://localhost:8080`. Migrations ran on startup — wallets are ready.

**Seeded wallets (balances are in minor units / cents):**

| Wallet ID | Balance |
| --------- | ------- |
| wallet_1  | 100000  |
| wallet_2  | 50000   |
| wallet_3  | 0       |

---

## Step 3 — Verify the server is alive

```bash
curl -s http://localhost:8080/healthz
```

Expected:

```json
{ "status": "ok" }
```

---

## Step 4 — Try all the scenarios

Run all these curl commands from a terminal. Each one shows a different feature.

### 4a. Happy path — successful transfer

```bash
curl -s -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":500}'
```

Expected — HTTP 201:

```json
{
  "transferId": "...",
  "status": "PROCESSED",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 500
}
```

wallet_1 balance goes from 100000 → 99500. wallet_2 goes from 50000 → 50500.

---

### 4b. Idempotency replay — same request again returns same result

Run the **exact same command** from 4a again:

```bash
curl -s -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":500}'
```

Expected — HTTP 201, **same transferId as before**:

```json
{
  "transferId": "...",
  "status": "PROCESSED",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 500
}
```

Money only moved once. The response is the stored one from the first call.

---

### 4c. Idempotency key reuse with different payload — rejected

```bash
curl -s -w "\nHTTP %{http_code}\n" -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":999}'
```

Expected — HTTP 422:

```json
{ "error": "idempotency key reused with different parameters" }
```

---

### 4d. Insufficient funds — transfer fails and is recorded

```bash
curl -s -w "\nHTTP %{http_code}\n" -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"k-insuf","fromWalletId":"wallet_3","toWalletId":"wallet_1","amount":100}'
```

Expected — HTTP 422:

```json
{ "transferId": "...", "status": "FAILED", "error": "insufficient funds" }
```

The transfer row is persisted with status FAILED. Balances are unchanged.

---

### 4e. Replay of a failed transfer — same error, no double debit

```bash
curl -s -w "\nHTTP %{http_code}\n" -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"k-insuf","fromWalletId":"wallet_3","toWalletId":"wallet_1","amount":100}'
```

Expected — HTTP 422, same transferId, same FAILED response:

```json
{ "transferId": "...", "status": "FAILED", "error": "insufficient funds" }
```

---

### 4f. Unknown wallet — 404

```bash
curl -s -w "\nHTTP %{http_code}\n" -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"k-nowallet","fromWalletId":"wallet_x","toWalletId":"wallet_1","amount":100}'
```

Expected — HTTP 404:

```json
{ "error": "wallet not found" }
```

---

### 4g. Validation errors — 400

Missing idempotency key:

```bash
curl -s -w "\nHTTP %{http_code}\n" -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

Expected — HTTP 400:

```json
{ "error": "invalid request: idempotency key is required" }
```

Zero or negative amount:

```bash
curl -s -w "\nHTTP %{http_code}\n" -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"k-zero","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":0}'
```

Expected — HTTP 400:

```json
{ "error": "amount must be greater than zero" }
```

Transfer to yourself:

```bash
curl -s -w "\nHTTP %{http_code}\n" -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"k-same","fromWalletId":"wallet_1","toWalletId":"wallet_1","amount":100}'
```

Expected — HTTP 400:

```json
{ "error": "from and to wallet must be different" }
```

---

## Step 5 — Run the fast tests (no Docker needed)

These run domain and handler tests only — no real DB.

```bash
make test-short
```

Expected:

```
Running fast tests (no Docker)...
ok  github.com/hariharandr/wallet-transfer-assignment/internal/config
ok  github.com/hariharandr/wallet-transfer-assignment/internal/domain
ok  github.com/hariharandr/wallet-transfer-assignment/internal/httpapi
ok  github.com/hariharandr/wallet-transfer-assignment/internal/platform/migrate
ok  github.com/hariharandr/wallet-transfer-assignment/internal/transfer
All fast tests passed!
```

---

## Step 6 — Run the full test suite (Docker required)

This runs integration tests with a real Postgres and the concurrency race tests.

```bash
make test
```

Expected output includes:

```
Running full test suite with race detector (Docker needed)...
go test ./... -race -count=1
?       github.com/hariharandr/wallet-transfer-assignment/cmd/server    [no test files]
?       github.com/hariharandr/wallet-transfer-assignment/internal/apperr       [no test files]
ok      github.com/hariharandr/wallet-transfer-assignment/internal/config       1.401s
ok      github.com/hariharandr/wallet-transfer-assignment/internal/domain       1.216s
ok      github.com/hariharandr/wallet-transfer-assignment/internal/httpapi      1.420s
ok      github.com/hariharandr/wallet-transfer-assignment/internal/platform/migrate     3.646s
?       github.com/hariharandr/wallet-transfer-assignment/internal/platform/postgres    [no test files]
ok      github.com/hariharandr/wallet-transfer-assignment/internal/transfer     10.546s
?       github.com/hariharandr/wallet-transfer-assignment/migrations    [no test files]
?       github.com/hariharandr/wallet-transfer-assignment/testsupport   [no test files]
All tests passed!
```

**What the concurrency test proves:**

- 20 goroutines all debit wallet_1 at the same time with different keys — final balance is exactly correct, no overdraft
- 15 goroutines fire the same key at the same time — exactly 1 transfer goes through, others get 409 in-progress or a replay

---

## Step 7 — Stop everything

```bash
# Ctrl+C the server (in the terminal running make run)

make db-down
```

---

## Design summary

### How transactions work

Every transfer runs in one Postgres transaction:

1. Lock both wallet rows with `SELECT ... FOR UPDATE` (ordered by id to prevent deadlock)
2. Insert transfer row as PENDING
3. Check balance — if not enough, mark FAILED and commit (so the failure is replayable)
4. Insert 2 ledger entries (DEBIT + CREDIT)
5. Update wallet balances
6. Mark transfer PROCESSED
7. Mark idempotency record COMPLETED with stored response
8. Commit

### Idempotency

Before the main transaction, the service does an `INSERT INTO idempotency_records ... ON CONFLICT DO NOTHING`. Whoever wins that insert does the work. Everyone else reads the existing record and either gets a stored response replay or a 409 if it's still in flight.

A sha256 fingerprint of (fromWalletId + toWalletId + amount) detects key reuse with different parameters.

### Concurrency safety

`SELECT ... FOR UPDATE` on both wallets, always in the same order (by wallet id). Two concurrent transfers on the same pair lock in the same order so neither blocks the other from starting — they just run one after the other. The `balance >= 0` DB check is an extra safety net behind the application-level check.

### Double-entry ledger

Every transfer writes exactly two ledger rows: one DEBIT from the source and one CREDIT to the destination. Foreign keys prevent orphan rows.

### Enums + code generation

Transfer and ledger statuses are Go `int` iota types with `//go:generate stringer`. The generated `String()` method is used to store human-readable TEXT in Postgres (`PROCESSED`, `FAILED`, `DEBIT`, `CREDIT`) via `driver.Valuer`/`sql.Scanner`. The generated files are committed so CI doesn't need the stringer binary.

### Tests

Docker-backed integration tests check `testing.Short()` and skip if the flag is set. This keeps CI green without Docker (`go test -short`). The full suite including concurrency runs locally with `make test`.

Here's a clean, honest AI Usage section you can drop into the README:

---

## AI Usage

I used Claude throughout this project as a research and decision-support tool, not as a code generator for the core logic.

**Where I used AI:**

- Researching patterns and methods — I asked Claude to compare approaches (e.g. advisory locks vs `SELECT FOR UPDATE`, idempotency strategies) and used those comparisons to make my own call
- Folder structure — I already had a mental model from prior Go projects; I used AI to sanity-check it, not to generate it
- Scoping — I asked Claude to estimate whether the core implementation was achievable in ~5 hours and where test coverage would eat the most time

**Decisions I made myself:**

- Storing money as integer minor units (cents) — I decided this upfront, AI confirmed it was standard practice
- Concurrency test design — 20 goroutines hammering the same wallet, 15 goroutines on the same idempotency key; I designed the scenario, AI helped me think through what to assert
- TDD approach — I chose to write domain and handler tests first before wiring up the DB layer
- Researching industry practices — I used Perplexity to search for blogs and articles on how financial institutions handle wallet transfers, double-entry ledger design, and idempotency in payment systems; some decisions (like committing FAILED transfers for replayability and the ledger structure) were influenced by what I read there

**Where AI did the heavy lifting:**

- `docker-compose.yml`, `Makefile`, and this `README.md` — fully AI-generated after I described what I needed
- Test completeness — I prompted Claude to go through every edge case (idempotency replay, failed transfer replay, concurrent overdraft, race detector) and generate tests that would catch gaps; I reviewed and ran them

The business logic, transaction flow, and architecture decisions are mine. AI handled the scaffolding and helped me not miss edge cases in tests.
