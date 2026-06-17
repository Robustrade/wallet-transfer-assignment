# Wallet Transfer Service — Backend Design Document

This document explains how the Spring Boot backend works: database schema, module structure, transfer flow, idempotency, concurrency, and API endpoints.

---

## 1. Technology stack

| Item | Choice |
|------|--------|
| Language | Java 17 |
| Framework | Spring Boot 3.4 |
| Persistence | Spring Data JPA |
| Database | H2 in-memory (PostgreSQL-compatible mode) |
| Validation | Jakarta Bean Validation |

**Run backend**

```bash
cd backend
./mvnw spring-boot:run        # Linux/Mac
.\mvnw.cmd spring-boot:run    # Windows
```

**Run tests**

```bash
cd backend
./mvnw test
```

API base URL: `http://localhost:8080/api`

---

## 2. Module structure (layered architecture)

The backend is organized by **domain module**, each with its own domain, dto, repository, service, and web layers where applicable.

```text
com.wallettransfer
├── WalletTransferApplication.java
├── config/                    # CORS, seed data
├── common/                    # Shared error handling
│   ├── dto/ErrorResponse
│   └── web/GlobalExceptionHandler
├── wallet/                    # Wallet balances & history
│   ├── domain/
│   ├── dto/
│   ├── repository/
│   ├── service/
│   └── web/WalletController
├── transfer/                  # Transfer orchestration
│   ├── domain/
│   ├── dto/
│   ├── repository/
│   ├── service/TransferService
│   └── web/TransferController
├── ledger/                    # Double-entry ledger
│   ├── domain/
│   ├── dto/
│   ├── repository/
│   └── service/LedgerService
└── idempotency/               # Duplicate request handling
    ├── domain/
    ├── repository/
    └── service/IdempotencyService
```

### Responsibility split

| Layer | Responsibility |
|-------|----------------|
| **web** | HTTP mapping, request validation, status codes |
| **service** | Business rules, orchestration, transactions |
| **repository** | Database access (JPA) |
| **domain** | Entities, enums, domain exceptions |
| **dto** | API request/response records |

`TransferService` is the main orchestrator. It delegates to:

- `WalletService` — balance checks, locking, debit/credit
- `LedgerService` — double-entry rows
- `IdempotencyService` — duplicate detection and cached responses

---

## 3. Database schema

Hibernate creates tables automatically (`ddl-auto: update`). Four main tables map to JPA entities.

### 3.1 `wallets`

Stores current balance per wallet (materialized balance, updated on each successful transfer).

| Column | Type | Notes |
|--------|------|-------|
| `id` | VARCHAR(64) | Primary key (`wallet_1`, etc.) |
| `balance` | BIGINT | Current balance (whole units) |
| `version` | BIGINT | Optimistic lock version |
| `updated_at` | TIMESTAMP | Last balance update |

**Entity:** `wallet.domain.Wallet`

- `debit(amount)` — throws `InsufficientBalanceException` if balance too low
- `credit(amount)` — increases balance

---

### 3.2 `transfers`

One row per transfer request. Tracks lifecycle state.

| Column | Type | Notes |
|--------|------|-------|
| `id` | UUID | Primary key |
| `from_wallet_id` | VARCHAR(64) | FK → wallets |
| `to_wallet_id` | VARCHAR(64) | FK → wallets |
| `amount` | BIGINT | Must be > 0 |
| `status` | VARCHAR | `PENDING`, `PROCESSED`, `FAILED` |
| `failure_reason` | VARCHAR(500) | Set when FAILED |
| `created_at` | TIMESTAMP | |
| `updated_at` | TIMESTAMP | |

**State machine**

```text
PENDING → PROCESSED   (successful transfer)
PENDING → FAILED      (e.g. insufficient balance)
```

**Entity:** `transfer.domain.Transfer`

---

### 3.3 `ledger_entries`

Double-entry bookkeeping. Every **successful** transfer creates exactly **two** rows.

| Column | Type | Notes |
|--------|------|-------|
| `id` | BIGSERIAL | Primary key |
| `transfer_id` | UUID | FK → transfers |
| `wallet_id` | VARCHAR(64) | FK → wallets |
| `entry_type` | VARCHAR | `DEBIT` or `CREDIT` |
| `amount` | BIGINT | Always positive |
| `created_at` | TIMESTAMP | |

**Unique constraint:** `(transfer_id, wallet_id, entry_type)` — prevents duplicate debit/credit for same transfer.

**Example** — transfer 100 from `wallet_1` → `wallet_2`:

| transfer_id | wallet_id | entry_type | amount |
|-------------|-----------|------------|--------|
| T1 | wallet_1 | DEBIT | 100 |
| T1 | wallet_2 | CREDIT | 100 |

**Entity:** `ledger.domain.LedgerEntry`

---

### 3.4 `idempotency_records`

Maps client idempotency keys to transfers and caches API responses.

| Column | Type | Notes |
|--------|------|-------|
| `idempotency_key` | VARCHAR(128) | **Primary key** (client UUID) |
| `transfer_id` | UUID | FK → transfers |
| `response_body` | CLOB/JSON | Serialized `TransferResponse` |
| `http_status` | INT | 200 or 422 |
| `created_at` | TIMESTAMP | |

**Entity:** `idempotency.domain.IdempotencyRecord`

The primary key on `idempotency_key` guarantees at most one transfer per key.

---

### 3.5 Entity relationship diagram

```text
┌─────────────┐         ┌──────────────┐         ┌─────────────────┐
│   wallets   │◄────────│   transfers  │────────►│ ledger_entries  │
└─────────────┘         └──────┬───────┘         └─────────────────┘
                               │
                               │
                        ┌──────▼──────────────┐
                        │ idempotency_records │
                        └─────────────────────┘
```

---

## 4. API endpoints

### Wallet module — `/api/wallets`

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/wallets` | List all wallets with balances |
| POST | `/api/wallets` | Create wallet `{ id, initialBalance }` |
| GET | `/api/wallets/{id}/history` | Wallet history, ledger, 90-day stats |

### Transfer module — `/api/transfers`

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/transfers` | Create transfer (idempotent) |
| GET | `/api/transfers` | List all transfers |
| GET | `/api/transfers/{id}/ledger` | Ledger entries for one transfer |

### Create transfer request

```json
POST /api/transfers
{
  "idempotencyKey": "550e8400-e29b-41d4-a716-446655440000",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

### Response (success)

```json
{
  "id": "uuid",
  "idempotencyKey": "550e8400-...",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "status": "PROCESSED",
  "failureReason": null,
  "duplicate": false,
  "createdAt": "2026-06-17T..."
}
```

HTTP status: **200** (processed) or **422** (failed, e.g. insufficient balance).

---

## 5. Transfer flow (step by step)

All steps run inside a single database transaction (`@Transactional` on `TransferService.createTransfer`).

```text
POST /api/transfers
        │
        ▼
┌───────────────────────────┐
│ 1. Validate request       │  from ≠ to, amount > 0
└─────────────┬─────────────┘
              ▼
┌───────────────────────────┐
│ 2. Idempotency lookup     │  key exists? → return cached response
└─────────────┬─────────────┘
              ▼
┌───────────────────────────┐
│ 3. Ensure wallets exist   │
└─────────────┬─────────────┘
              ▼
┌───────────────────────────┐
│ 4. Create transfer        │  status = PENDING
└─────────────┬─────────────┘
              ▼
┌───────────────────────────┐
│ 5. Reserve idempotency key│  unique constraint prevents races
└─────────────┬─────────────┘
              ▼
┌───────────────────────────┐
│ 6. Lock wallets (FOR UPDATE) │
│    Debit source           │
│    Credit destination     │
└─────────────┬─────────────┘
              ▼
┌───────────────────────────┐
│ 7. Insert ledger entries  │  DEBIT + CREDIT
└─────────────┬─────────────┘
              ▼
┌───────────────────────────┐
│ 8. Mark PROCESSED / FAILED│
└─────────────┬─────────────┘
              ▼
┌───────────────────────────┐
│ 9. Cache response in      │
│    idempotency_records    │
└───────────────────────────┘
```

### Failure: insufficient balance

1. `Wallet.debit()` throws `InsufficientBalanceException`
2. Transfer marked `FAILED` with reason
3. No ledger entries created
4. Balances unchanged
5. Response cached with HTTP 422

`@Transactional(noRollbackFor = InsufficientBalanceException.class)` ensures the FAILED state and idempotency record are **committed**, not rolled back.

---

## 6. Idempotency strategy

**Goal:** Same `idempotencyKey` → same API result, no double spend.

### How it works

1. **First request** with key `abc-123`:
   - Creates `Transfer` + `IdempotencyRecord`
   - Processes transfer
   - Stores JSON response in `idempotency_records.response_body`

2. **Duplicate request** with same key:
   - Finds existing `IdempotencyRecord`
   - Returns cached JSON with `duplicate: true`
   - No new debit/credit

### Concurrent duplicate requests

Two parallel requests with the same key:

1. Both may pass initial lookup
2. First `INSERT` into `idempotency_records` wins
3. Second hits unique constraint → `DataIntegrityViolationException`
4. Second request loads existing record and returns cached result

### Retry after partial failure

If a request crashes after creating the idempotency record but before caching the response:

- Retry finds the record with `response_body = null`
- Re-runs `executeTransfer` on the existing `PENDING` transfer
- Safe because wallet lock + status check prevent double processing

---

## 7. Concurrency strategy

**Problem:** Two transfers debiting the same wallet at the same time must not overdraw.

**Solution:** Pessimistic row-level locking.

```java
@Lock(LockModeType.PESSIMISTIC_WRITE)
@Query("SELECT w FROM Wallet w WHERE w.id IN :ids ORDER BY w.id")
List<Wallet> findByIdsForUpdate(@Param("ids") List<String> ids);
```

### Rules

1. Lock **both** wallets inside the same DB transaction
2. Lock in **sorted ID order** (`wallet_1` before `wallet_2`) to avoid deadlocks
3. Check balance and update inside the lock
4. Commit atomically with ledger entries

### What this prevents

| Scenario | Result |
|----------|--------|
| Two transfers from same wallet, enough total balance for one | One succeeds, one fails |
| Duplicate idempotency retry | No second debit |
| Parallel different keys | Serialized by wallet row locks |

---

## 8. Seed data

On startup, `DataInitializer` creates wallets if the table is empty:

| Wallet | Balance |
|--------|---------|
| wallet_1 | 1000 |
| wallet_2 | 500 |
| wallet_3 | 250 |

---

## 9. Tests

Integration tests in `TransferIntegrationTest`:

| Test | Verifies |
|------|----------|
| `createsTransferWithLedgerEntries` | Happy path, balances, 2 ledger rows |
| `duplicateIdempotencyKeyReturnsSameResultWithoutDoubleSpend` | Idempotency, no double spend |
| `failsWhenBalanceIsInsufficient` | FAILED status, no ledger, balance unchanged |
| `listsWallets` | GET /api/wallets |

---

## 10. Tradeoffs and assumptions

| Decision | Rationale |
|----------|-----------|
| H2 in-memory | Easy local dev; schema is PostgreSQL-compatible |
| Stored balance + ledger | Fast balance reads; ledger provides audit trail |
| Pessimistic locking | Simple and correct for assignment scope |
| Cached JSON response | Exact replay of API response for duplicates |
| Amounts as whole numbers | Simplicity; no floating-point issues |
| UTC timestamps | Consistent date handling for charts/history |

### Possible production improvements

- PostgreSQL with Flyway migrations instead of `ddl-auto: update`
- Idempotency key scoped per client/tenant
- Outbox pattern for async notifications
- Metrics and structured logging
- Explicit `CHECK (balance >= 0)` at DB level

---

## 11. Frontend (reference)

Angular app in `frontend/` proxies `/api` to `localhost:8080` for testing transfers, wallet history, ledger, and analytics UI.

See `SOLUTION.md` for run instructions.
