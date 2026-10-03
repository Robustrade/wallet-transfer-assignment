# Wallet Transfer Service

A reliable, transactional wallet-to-wallet transfer backend service built in **Go** using the **Echo** framework, **GORM**, and **PostgreSQL**.

The service provides financial-grade consistency guarantees:
* **API-Level Exactly-Once Semantics (Idempotency)**: Guarded via durable idempotency records, SHA-256 payload hashing, and exact original response replay.
* **Double-Entry Bookkeeping**: Every transfer produces matched, immutable debit and credit ledger records that always balance to zero.
* **Pessimistic Concurrency Control**: Strict row-level locking (`SELECT ... FOR UPDATE`) with **deterministic lock ordering** to eliminate circular wait deadlocks.
* **Overdraft & Race Safety**: Enforced via atomic database transactions, lock serialization, and database `CHECK (balance >= 0)` constraints.
* **Single Connection String Configuration**: Simple configuration via `config.json`, `.env`, or the `DATABASE_URL` environment variable.

---

## 1. Architecture Overview

The system follows **Clean Layered Architecture** with a unified repository pattern:

```
wallet-transfer-assignment/
├── cmd/
│   └── api/
│       └── main.go                 # App entry point, unified repo wiring & HTTP server
├── internal/
│   ├── config/                     # Configuration loader (config.json / .env / env vars)
│   ├── database/                   # GORM DB connection pool & Auto-migrations
│   ├── domain/                     # Entities, status constants, domain errors
│   ├── handler/                    # Thin Echo HTTP controllers
│   ├── repository/                 # Repository interfaces
│   │   └── postgres/               # GORM implementation with deterministic row locking
│   └── service/                    # Transfer orchestration & ledger reconciliation
├── config.json                     # Default configuration file (single connection string)
├── config.example.json             # Example JSON config template
├── .env.example                    # Example environment file template
├── DESIGN.md                       # Comprehensive architectural specification
├── docker-compose.yml              # PostgreSQL + App container setup
├── Dockerfile                      # Multi-stage production container build
└── Makefile                        # Common developer commands
```

### Layer Separation
1. **Handler (`internal/handler`)**: Request binding, input validation, HTTP status mapping.
2. **Service (`internal/service`)**: Business rules, idempotency checking, transfer workflow orchestration.
3. **Repository (`internal/repository/postgres`)**: Unified repository managing database queries, atomic transactions (`ExecuteInTransaction`), and pessimistic row locks.
4. **Domain (`internal/domain`)**: Core entities (`Wallet`, `Transfer`, `LedgerEntry`, `IdempotencyRecord`) and domain error sentinels.

---

## 2. Configuration Guide

The service loads configuration with the following precedence:
1. **Defaults**
2. **`config.json`** (or path set in `CONFIG_FILE`)
3. **`.env`** file (if present)
4. **Environment Variables** (highest precedence)

### Configuration Properties

| JSON Key | Environment Variable | Default | Description |
| :--- | :--- | :--- | :--- |
| `service_name` | `SERVICE_NAME` | `wallet-transfer-service` | Service identifier |
| `port` | `PORT` | `8080` | HTTP listen port |
| `environment` | `ENVIRONMENT` | `development` | Runtime environment |
| `log_level` | `LOG_LEVEL` | `info` | Logger verbosity |
| `database_url` | `DATABASE_URL` | `postgres://test:test@localhost:5432/wallet_transfer?sslmode=disable` | Single database connection string |

### Example `config.json`
```json
{
  "service_name": "wallet-transfer-service",
  "port": "8080",
  "environment": "development",
  "log_level": "info",
  "database_url": "postgres://test:test@localhost:5432/wallet_transfer?sslmode=disable"
}
```

---

## 3. How to Run

### Option A: Local PostgreSQL (e.g. Homebrew)
1. Ensure PostgreSQL is running:
   ```bash
   brew services start postgresql@18
   ```
2. Create the database:
   ```bash
   createdb wallet_transfer
   ```
3. Update `database_url` in `config.json` or export `DATABASE_URL`:
   ```bash
   export DATABASE_URL="postgres://$(whoami)@localhost:5432/wallet_transfer?sslmode=disable"
   ```
4. Start the server:
   ```bash
   go run ./cmd/api/main.go
   ```

### Option B: Docker Compose
Spins up PostgreSQL 16 and the service container:
```bash
docker compose up --build
```
The service will be live on `http://localhost:8080`.

To stop:
```bash
docker compose down -v
```

### Option C: Local Go with SQLite (Zero External Dependencies)
You can run locally with an embedded SQLite database by specifying an SQLite connection string:
```bash
DATABASE_URL="sqlite://wallet.db" go run ./cmd/api/main.go
```

---

## 4. API Endpoints & Examples

### Health Check
```bash
curl -X GET http://localhost:8080/health
```
```json
{
  "service": "wallet-transfer-service",
  "status": "ok"
}
```

### 1. Create Wallets
```bash
# Create Wallet 1 (Alice) with $10.00 (1000 cents)
curl -X POST http://localhost:8080/wallets \
  -H "Content-Type: application/json" \
  -d '{"id": "w_alice", "initialBalance": 1000, "currency": "USD"}'

# Create Wallet 2 (Bob) with $2.00 (200 cents)
curl -X POST http://localhost:8080/wallets \
  -H "Content-Type: application/json" \
  -d '{"id": "w_bob", "initialBalance": 200, "currency": "USD"}'
```

### 2. Execute Transfer (`POST /transfers`)
```bash
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "tx-client-001",
    "fromWalletId": "w_alice",
    "toWalletId": "w_bob",
    "amount": 300,
    "currency": "USD"
  }'
```
**Response (201 Created):**
```json
{
  "transferId": "tr_3b378076-eb34-4ff5-b9d9-bb3f4ea2476e",
  "idempotencyKey": "tx-client-001",
  "fromWalletId": "w_alice",
  "toWalletId": "w_bob",
  "amount": 300,
  "currency": "USD",
  "status": "PROCESSED",
  "createdAt": "2026-10-02T16:25:00Z"
}
```

### 3. Replay Transfer with Identical Key
Calling the endpoint again with the same `idempotencyKey` returns `200 OK` with the exact same response without re-executing the transfer or producing duplicate ledger rows.

### 4. Inspect Wallet Balance & Ledger Audit
```bash
curl -X GET http://localhost:8080/wallets/w_alice/balance
```
```json
{
  "walletId": "w_alice",
  "storedBalance": 700,
  "ledgerBalance": 700,
  "isAuditConsistent": true,
  "currency": "USD"
}
```

### 5. Inspect Double-Entry Ledger for Transfer
```bash
curl -X GET http://localhost:8080/transfers/tr_3b378076-eb34-4ff5-b9d9-bb3f4ea2476e/ledger
```
```json
{
  "transferId": "tr_3b378076-eb34-4ff5-b9d9-bb3f4ea2476e",
  "entries": [
    {
      "id": "led_debit_001",
      "walletId": "w_alice",
      "transferId": "tr_3b378076-eb34-4ff5-b9d9-bb3f4ea2476e",
      "type": "DEBIT",
      "amount": 300,
      "balanceAfter": 700,
      "createdAt": "2026-10-02T16:25:00Z"
    },
    {
      "id": "led_credit_001",
      "walletId": "w_bob",
      "transferId": "tr_3b378076-eb34-4ff5-b9d9-bb3f4ea2476e",
      "type": "CREDIT",
      "amount": 300,
      "balanceAfter": 500,
      "createdAt": "2026-10-02T16:25:00Z"
    }
  ]
}
```

---

## 5. Concurrency & Deadlock Prevention Strategy

### The Problem
When concurrent transfers occur in opposing directions (Wallet A $\to$ Wallet B and Wallet B $\to$ Wallet A simultaneously), unordered row locks cause circular waits leading to PostgreSQL `40P01 deadlocks detected`.

### The Solution: Deterministic Lock Ordering
Both wallets are locked inside the database transaction using `clause.Locking{Strength: "UPDATE"}` in **strict lexicographical order of their wallet IDs** (`firstID = min(A, B)`, `secondID = max(A, B)`):
```go
firstID, secondID := fromID, toID
if firstID > secondID {
    firstID, secondID = toID, fromID
}
// 1. Lock first wallet
w1, err := r.GetForUpdate(ctx, tx, firstID)
// 2. Lock second wallet
w2, err := r.GetForUpdate(ctx, tx, secondID)
```
Because every transaction always locks resources in identical global order, circular wait deadlocks are mathematically eliminated.

---

## 6. Testing

### Run All Tests
```bash
go test -v ./...
```

### Run Concurrency & Race Detector Tests
```bash
go test -v -race ./...
```

### Run Tests with Coverage Report
```bash
make test-coverage
```

### Key Test Scenarios Covered
* **`TestTransferService_Success`**: Verifies atomic balance adjustment and exactly 2 ledger entries created.
* **`TestTransferService_IdempotencyReplay`**: Replaying the same key returns cached response without deducting balance twice.
* **`TestTransferService_IdempotencyPayloadMismatch`**: Prevents key recycling with different amounts or recipients (returns 422).
* **`TestTransferService_InsufficientBalance`**: Sets transfer state to `FAILED`, leaves balance intact, creates 0 ledger entries.
* **`TestTransferService_ConcurrencyNoDoubleSpend`**: 10 concurrent goroutines racing to debit a wallet with limited capacity; prevents overdraft and maintains audit parity.
* **`TestTransferService_ConcurrencyCrossTransfersNoDeadlock`**: 40 concurrent opposing transfers (A $\to$ B and B $\to$ A) running simultaneously without deadlocks.
