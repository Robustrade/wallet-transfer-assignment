# Wallet Transfer Service

A high-concurrency wallet transfer service written in Go and PostgreSQL. It guarantees safe money movement with zero overdrafts, deadlock-free execution under concurrent bidirectional transfers, and durable idempotency for network retries.

---

## How It Works

Here is the lifecycle of a `POST /transfers` request:

```text
Client Request: POST /transfers
      │
      ▼
1. Validate Request
   └── Amount > 0, source != destination wallet, IDs not empty
      │
      ▼
2. Check Idempotency (Fast-Path, No Locks)
   ├── Key exists with SAME payload?       ──► Return 200 OK (Idempotent-Replay: true)
   └── Key exists with DIFFERENT payload?  ──► Return 409 Conflict
      │
      ▼ (New Key)
3. Begin PostgreSQL Transaction
   │
   ├── Step A: Deterministic Row Locking
   │     SELECT ... WHERE id IN ($from, $to) ORDER BY id FOR UPDATE;
   │     (Locks wallets alphabetically to eliminate deadlocks)
   │
   ├── Step B: Validate Business Rules
   │     - Wallets exist and currencies match
   │     - Source wallet has sufficient balance
   │
   ├── Step C: Update Balances
   │     UPDATE wallets SET balance = balance - amount WHERE id = $from
   │     UPDATE wallets SET balance = balance + amount WHERE id = $to
   │     (DB constraint CHECK (balance >= 0) prevents overdrafts)
   │
   ├── Step D: Insert Double-Entry Ledger Rows
   │     INSERT INTO ledger_entries:
   │       - DEBIT  from_wallet (amount)
   │       - CREDIT to_wallet   (amount)
   │
   ├── Step E: Record Transfer & Idempotency Key
   │     INSERT INTO transfers (status: 'PROCESSED')
   │     INSERT INTO idempotency_records (key, payload_hash, response_body)
   │
   └── Step F: Commit Transaction
      │
      ▼
4. Return 201 Created (with transfer ID and details)
```

---

## Core Guarantees

### 1. Deadlock-Free Concurrency
When two transfers run simultaneously between the same two accounts in opposite directions:
- Transfer 1: `wallet_A` ➔ `wallet_B`
- Transfer 2: `wallet_B` ➔ `wallet_A`

Naive implementations lock the sender first, causing a circular deadlock:
```text
Tx 1: Locks A ──(wants B)──► [WAITING]
Tx 2: Locks B ──(wants A)──► [DEADLOCK - Postgres kills one]
```

This service queries wallets with `ORDER BY id FOR UPDATE`:
```text
Tx 1: Requests lock on A, then B
Tx 2: Requests lock on A, then B  <-- Always same alphabetical order!

Result: Tx 1 acquires A and B, executes, and commits.
        Tx 2 waits cleanly on A, then executes. Zero deadlocks.
```

### 2. Zero Overdrafts
Balances are stored in integer minor units (paise/cents) to avoid floating-point rounding errors. Overdrafts are prevented at two independent layers:
1. **Application domain logic**: `source.balance >= amount` check before executing the debit.
2. **Database constraint**: `CHECK (balance >= 0)` on the `wallets` table. Even if a bug existed in the application code, PostgreSQL will reject any transaction that results in a negative balance.

### 3. Safe Network Retries (Idempotency)
Every transfer requires an `idempotencyKey`:
- **Retry with identical payload**: Returns the original `200 OK` response with `Idempotent-Replay: true` header. No duplicate transfers or ledger entries.
- **Reuse key with different payload**: Returns `409 Conflict` (`IDEMPOTENCY_KEY_CONFLICT`).
- **Atomic storage**: Idempotency records are written inside the *same* database transaction as the balance update. No two-phase commit or Redis/DB desync.

### 4. Double-Entry Accounting
Every transfer creates exactly two immutable ledger rows: one `DEBIT` and one `CREDIT`. The sum of all ledger entries for a transfer is always zero. Ledger rows have an `ON DELETE RESTRICT` constraint, ensuring an immutable audit trail.

---

## Quickstart

### Prerequisites
- [Go 1.24+](https://golang.org/dl/)
- [Docker & Docker Compose](https://docs.docker.com/compose/)

### 1. Start the Database
```bash
make db
```
Starts PostgreSQL 16 on port `5432`.

### 2. Run Migrations & Seed Test Wallets
```bash
make seed
```
Applies migrations and seeds 4 test wallets:

| Wallet ID | Currency | Starting Balance | Purpose |
|---|---|---|---|
| `wallet_1` | INR | ₹1,000.00 (100,000 paise) | Funded source |
| `wallet_2` | INR | ₹500.00 (50,000 paise) | Funded destination |
| `wallet_3` | INR | ₹0.00 | Empty wallet (test insufficient funds) |
| `wallet_usd` | USD | $250.00 | USD wallet (test currency mismatch) |

### 3. Start the API Server
```bash
make run
```
Server listens on `http://localhost:8080`.

---

## Verification & Tests

```bash
# Run unit and service integration tests
make test

# Run tests with the Go race detector enabled
make test-race

# Run end-to-end smoke test suite (against running server)
make smoke
```

---

## API Examples

### 1. Create a Transfer
```bash
curl -i -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "tx-101",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 2500
  }'
```

**Response (`201 Created`):**
```json
{
  "transferId": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
  "status": "PROCESSED",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 2500,
  "currency": "INR",
  "createdAt": "2026-09-05T08:30:00Z"
}
```

*Sending the identical request again returns `200 OK` with header `Idempotent-Replay: true`.*

### 2. View Transfer & Double-Entry Ledger
```bash
curl -s http://localhost:8080/transfers/9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d
```

**Response (`200 OK`):**
```json
{
  "transferId": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
  "status": "PROCESSED",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 2500,
  "currency": "INR",
  "createdAt": "2026-09-05T08:30:00Z",
  "ledgerEntries": [
    {
      "entryId": 1,
      "walletId": "wallet_1",
      "type": "DEBIT",
      "amount": 2500,
      "currency": "INR",
      "createdAt": "2026-09-05T08:30:00Z"
    },
    {
      "entryId": 2,
      "walletId": "wallet_2",
      "type": "CREDIT",
      "amount": 2500,
      "currency": "INR",
      "createdAt": "2026-09-05T08:30:00Z"
    }
  ]
}
```

### 3. Check Wallet Balance
```bash
curl -s http://localhost:8080/wallets/wallet_1
```

**Response (`200 OK`):**
```json
{
  "walletId": "wallet_1",
  "currency": "INR",
  "balance": 97500,
  "updatedAt": "2026-09-05T08:30:00Z"
}
```

### 4. Wallet Transfer History
```bash
curl -s "http://localhost:8080/wallets/wallet_1/transfers?limit=10"
```

### 5. Health & Metrics
- `GET /healthz` - Process liveness check (`{"status":"ok"}`)
- `GET /readyz` - Database readiness probe (`{"status":"ready"}`)
- `GET /metrics` - Prometheus metrics (`http_requests_total`, `transfer_duration_seconds`, etc.)

---

## Useful Commands

| Command | Action |
|---|---|
| `make db` | Start PostgreSQL 16 container |
| `make seed` | Run migrations and insert demo seed wallets |
| `make run` | Start HTTP server on `:8080` |
| `make test` | Run all Go unit and integration tests |
| `make test-race` | Run test suite with `-race` detection |
| `make smoke` | Run curl-based end-to-end smoke test script |
| `make fmt` | Format all Go code |

---

## Project Structure

```text
├── cmd/
│   ├── seed/             # Database migration and demo seeder CLI
│   └── server/           # Main HTTP application entrypoint
├── internal/
│   ├── domain/           # Core models, validations, and state machines
│   ├── service/          # Transfer orchestration & business logic
│   ├── repository/       # PostgreSQL queries, locking, and transactions
│   ├── http/             # Gin handlers, routes, and middleware
│   ├── metrics/          # Prometheus RED metrics instrumentation
│   └── config/           # Environment configuration
├── migrations/           # PostgreSQL schema migrations
├── scripts/
│   └── smoke.sh          # End-to-end automated smoke tests
├── api/
│   └── openapi.yaml      # Full OpenAPI 3.0 specification
├── docs/
│   ├── DESIGN.md         # Deep-dive architecture & concurrency design
│   ├── CHECKLIST.md      # Requirements verification checklist
│   └── AI_USAGE.md       # AI assistant disclosure log
├── Makefile
└── docker-compose.yml
```
