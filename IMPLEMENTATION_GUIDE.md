# Implementation Guide

**Goal:** Build one endpoint — `POST /transfers` — that moves money between wallets safely (no double-spend, no duplicate transfers on retry).

**Track progress:** [`IMPLEMENTATION_CHECKLIST.md`](./IMPLEMENTATION_CHECKLIST.md)  
**Requirements source:** [`ASSIGNMENT.md`](./ASSIGNMENT.md)

---

## What you are building (one picture)

```
Client  →  POST /transfers  →  Handler  →  Service  →  Repository  →  PostgreSQL
                                    ↑            ↑
                              JSON in/out    idempotency + locking + ledger
```

**The service must do this on every request:**

1. If `idempotencyKey` was seen before → return the **same response** (no new transfer).
2. Else create a transfer, debit source wallet, credit destination wallet, write 2 ledger rows, mark `PROCESSED`.
3. If not enough money → mark `FAILED`, return error, **do not** change balances.
4. All of step 2 or 3 must happen inside **one database transaction** with **row locks** on wallets.

---

## Where you are now

| Done | Not done yet |
|------|----------------|
| `go.mod` | All Go code (`internal/` is empty) |
| `docker-compose.yml` + Postgres | Wire DB in `main.go` |
| Migrations (`000001` schema, `000002` seed wallets) | `Makefile` targets for run/test |
| `Makefile` (db + migrate) | Handler, service, repository |
| `.env.example` | Tests |
| Empty `cmd/server/main.go` | README update + PR |

**Start at Step 5 below** (domain models). Steps 1–4 are mostly complete.

---

## Step-by-step: do these in order

### Step 1 — Git branch

```bash
git checkout main
git pull
git checkout -b solution/<your-name>
```

**Why:** Assignment requires a PR from a feature branch, not direct commits to `main`.

---

### Step 2 — Run database locally

```bash
make db-up
make migrate-up
```

**Verify:** Postgres is up; tables `wallets`, `transfers`, `ledger_entries`, `idempotency_records` exist; seed wallets are inserted.

**Why:** Every feature depends on the schema. You already have migration files — just run them.

---

### Step 3 — Create folder structure

Create these packages (empty files with `package` declaration is fine to start):

```
cmd/server/main.go          ← already exists
internal/
  config/config.go          ← read HTTP_ADDR, DATABASE_URL from env
  domain/                   ← structs + errors + validation
  handler/                  ← HTTP for POST /transfers
  service/                  ← transfer business logic
  repository/               ← SQL queries
```

**Why:** Assignment requires handler → service → repository → domain layers.

---

### Step 4 — Add Go dependencies

In `go.mod`, add (typical choices):

- `github.com/jackc/pgx/v5` or `database/sql` + `github.com/lib/pq` — Postgres
- `github.com/google/uuid` — transfer IDs

**Why:** You need a real DB driver. CI runs `go test ./... -race`.

---

### Step 5 — Domain layer (`internal/domain/`)

**Create these files and types:**

| File | What to put in it |
|------|-------------------|
| `wallet.go` | `Wallet` struct: `ID`, `Balance`, timestamps |
| `transfer.go` | `Transfer`, `TransferRequest`, `TransferResponse` |
| `ledger.go` | `LedgerEntry`, constants `DEBIT` / `CREDIT` |
| `status.go` | Constants: `PENDING`, `PROCESSED`, `FAILED` |
| `errors.go` | Sentinel errors: `ErrInsufficientFunds`, `ErrInvalidAmount`, etc. |

**Implement:**

- `TransferRequest.Validate()` — reject empty keys, `amount <= 0`, same from/to wallet.

**Why:** Pure business types with no HTTP or SQL. Handlers and repos both use these.

---

### Step 6 — Repository interfaces + Postgres impl (`internal/repository/`)

**Define what the service needs (interfaces):**

```go
type TransferRepository interface {
    FindByIdempotencyKey(ctx, key) (*Transfer, error)
    FindIdempotencyRecord(ctx, key) (response JSON, httpStatus, error)
    CreateTransfer(ctx, tx, transfer) error
    UpdateTransferStatus(ctx, tx, id, status) error
    InsertLedgerEntries(ctx, tx, entries) error
    SaveIdempotencyRecord(ctx, tx, key, transferID, response, status) error
}

type WalletRepository interface {
    GetForUpdate(ctx, tx, walletID) (*Wallet, error)   // SELECT ... FOR UPDATE
    UpdateBalance(ctx, tx, walletID, delta int64) error
}
```

**Add `tx.go`:**

```go
WithTx(ctx, func(ctx, tx) error) error   // BEGIN / COMMIT / ROLLBACK
```

**Implement each method with SQL** matching your migration schema.

**Why:** Repositories only talk to the DB. Service owns business rules.

---

### Step 7 — Transfer service (`internal/service/transfer_service.go`)

**Implement `CreateTransfer(ctx, req) (TransferResponse, error)` with this exact flow:**

```
A. Validate request (domain.Validate)

B. Look up idempotency_key in idempotency_records (or transfers)
   → if found: return stored response, STOP

C. WithTx {
     C1. Look up idempotency again (inside tx, for race safety)
     C2. Insert transfer row (status = PENDING)
         → if UNIQUE violation on idempotency_key: load existing, return it
     C3. Lock source wallet: SELECT ... FOR UPDATE
     C4. Lock destination wallet (lock in sorted ID order to avoid deadlock)
     C5. If source.balance < amount:
           update transfer → FAILED
           save idempotency record (409 + error body)
           return ErrInsufficientFunds
     C6. Debit source, credit destination
     C7. Insert 2 ledger entries (DEBIT on source, CREDIT on dest)
     C8. Update transfer → PROCESSED
     C9. Save idempotency record (201 + success body)
   }

D. Return response
```

**Why:** This is the core of the assignment — idempotency, atomicity, locking, ledger, state machine.

---

### Step 8 — HTTP handler (`internal/handler/transfer_handler.go`)

**Implement `POST /transfers`:**

1. Decode JSON body.
2. Call `service.CreateTransfer`.
3. Map errors to HTTP:

| Error | HTTP |
|-------|------|
| Validation errors | `400` |
| Insufficient funds | `409` |
| Success | `201` |
| Unknown | `500` |

**Do not** put SQL or balance logic in the handler.

**Why:** Handler is a thin adapter between HTTP and service.

---

### Step 9 — Wire everything in `main.go`

**In `cmd/server/main.go`:**

1. Load config from env.
2. Open Postgres connection pool.
3. Create repositories → service → handler.
4. Register route: `POST /transfers`.
5. Start server on `:8080` (or `HTTP_ADDR`).
6. Optional: graceful shutdown on SIGTERM.

**Manual test:**

```bash
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"test-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

Run the same curl again — response must be **identical**, balance must not change twice.

---

### Step 10 — Tests

**Write these tests (minimum):**

| Test | Assert |
|------|--------|
| Happy path | Balances change, status `PROCESSED`, 2 ledger rows |
| Idempotency | Same key twice → same response, 1 transfer only |
| Insufficient funds | `409`, status `FAILED`, balances unchanged |
| Bad input | Missing fields → `400` |
| Concurrency (bonus) | Parallel debits from same wallet → no negative balance |

**Where:**
- Unit tests: mock repository interfaces, test service validation.
- Integration tests: real Postgres (docker), test full flow.

**Run before PR:**

```bash
go test ./... -race -cover
golangci-lint run ./...
gofmt -l .
```

---

### Step 11 — README

Add to `README.md`:

- Prerequisites (Go, Docker)
- `make db-up && make migrate-up`
- `go run ./cmd/server`
- Example `curl`
- How to run tests

**Why:** Reviewers must be able to run your code.

---

### Step 12 — Open PR

1. Push branch `solution/<your-name>`.
2. Open PR into `main`.
3. Fill [`.github/pull_request_template.md`](./.github/pull_request_template.md):
   - Schema design
   - Idempotency strategy (explain Step 7 flow)
   - Concurrency strategy (`FOR UPDATE`, lock ordering)
   - How to run / test
   - AI disclosure
4. Use small commits: `feat: add schema`, `feat: implement transfer service`, `test: idempotency`.

---

## API contract (reference)

**Request:**

```json
POST /transfers
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

**Success response (`201`):**

```json
{
  "id": "<uuid>",
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "status": "PROCESSED"
}
```

**Amounts:** whole rupees (INR) as `int64` / `BIGINT`. Never use `float`.

---

## Database tables (reference)

You already have these in `migrations/000001_init_schema.up.sql`:

| Table | Purpose |
|-------|---------|
| `wallets` | Current balance per wallet |
| `transfers` | One row per transfer; `idempotency_key UNIQUE` |
| `ledger_entries` | DEBIT + CREDIT audit trail per transfer |
| `idempotency_records` | Cached HTTP response for replay |

---

## Design patterns to use (keep it simple)

| Pattern | Where |
|---------|--------|
| Layered architecture | handler / service / repository / domain |
| Repository | `internal/repository` |
| Dependency injection | wire in `main.go` |
| Unit of Work | `WithTx()` wraps BEGIN/COMMIT |
| State machine | `PENDING → PROCESSED \| FAILED` on `Transfer` |
| Idempotency key | UNIQUE constraint + lookup before side effects |

**Do not add:** CQRS, event sourcing, microservices, generic workflow engines — overkill for this assignment.

---

## Common mistakes (avoid these)

| Mistake | Correct approach |
|---------|------------------|
| Idempotency map in memory | Store in PostgreSQL |
| Check balance outside transaction | Check after `FOR UPDATE` inside tx |
| Only update balances, no ledger | Always insert DEBIT + CREDIT |
| Handler calls SQL directly | Handler → service → repository |
| Same idempotency key creates 2 transfers | UNIQUE constraint + handle conflict |
| Lock wallets in random order | Lock by sorted wallet ID |

---

## Definition of done

You are ready to submit when:

- [ ] `POST /transfers` works against Postgres
- [ ] Retry with same `idempotencyKey` returns same response, no double debit
- [ ] Every successful transfer has exactly 2 ledger entries
- [ ] Concurrent debits cannot overdraw a wallet
- [ ] `go test ./... -race -cover` passes
- [ ] Lint + format pass
- [ ] README + PR description complete
- [ ] You can explain the flow in Step 7 without reading code

---

## Need the "why" behind a step?

| Topic | Short reason |
|-------|----------------|
| Idempotency | Clients retry on timeout — without it, retries double-charge |
| Double-entry ledger | Financial audit standard; proves where money went |
| `FOR UPDATE` | Stops two requests reading the same balance and both spending it |
| DB UNIQUE on idempotency | Last defense if application logic has a bug |
| Layered architecture | Testable, matches what interviewers evaluate |
| Integer amounts | Floats cause rounding bugs in money code |

For the full rubric interviewers use, see [`evaluation_guide.md`](./evaluation_guide.md).
