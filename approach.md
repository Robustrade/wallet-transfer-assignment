# Wallet Transfer Service — Approach

## Tech Stack

| Concern | Choice |
|---|---|
| Language | Java 17 |
| Framework | Spring Boot 4.0.6 |
| Database | PostgreSQL |
| Data access | Raw JDBC via `JdbcTemplate` |
| Build | Maven |

---

## Database Schema

```sql
CREATE TYPE entry_type AS ENUM ('DEBIT', 'CREDIT');
```
PostgreSQL native enum — used in `ledger_entries.type`. Enforced at the DB level; no invalid values possible.

---

### `wallets`
```sql
CREATE TABLE wallets (
    id          BIGSERIAL PRIMARY KEY,
    balance     BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

```
- `BIGSERIAL` — auto-incrementing integer PK (1, 2, 3 …)
- Wallet IDs in the API become integers (e.g. `1`, `2`) instead of strings
- Balance is stored directly on the wallet row (not derived from ledger)
- Updated atomically inside the same transaction as ledger entries
- Transfers are rejected if `balance < amount` (no overdraft)

---

### `transfers`
```sql
CREATE TABLE transfers (
    id                TEXT PRIMARY KEY,
    idempotency_key   TEXT UNIQUE NOT NULL,
    from_wallet_id    BIGINT NOT NULL REFERENCES wallets(id),
    to_wallet_id      BIGINT NOT NULL REFERENCES wallets(id),
    amount            BIGINT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'PENDING',
    failure_reason    TEXT,
    created_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet   ON transfers(to_wallet_id);
```
- Transfer `id` stays as UUID TEXT (external-facing identifier)
- `from_wallet_id` / `to_wallet_id` are now `BIGINT` foreign keys
- PostgreSQL does **not** auto-create indexes on foreign keys (unlike MySQL) — explicit indexes added for join/filter performance
- `status` follows the state machine: `PENDING → PROCESSED` or `PENDING → FAILED`
- `idempotency_key` UNIQUE constraint creates its own index automatically

---

### `ledger_entries`
```sql
CREATE TABLE ledger_entries (
    id            BIGSERIAL PRIMARY KEY,
    transfer_id   TEXT NOT NULL REFERENCES transfers(id),
    wallet_id     BIGINT NOT NULL REFERENCES wallets(id),
    type          entry_type NOT NULL,
    amount        BIGINT NOT NULL,
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_ledger_wallet   ON ledger_entries(wallet_id);
CREATE INDEX idx_ledger_transfer ON ledger_entries(transfer_id);
```
- `type` is the native `entry_type` enum (`DEBIT` / `CREDIT`) — not a raw string
- `wallet_id` is `BIGINT` (matches wallets PK)
- Every transfer produces exactly 2 entries: `DEBIT` from source, `CREDIT` to destination
- Entries are inserted only after both wallet balance updates succeed

---

### `idempotency_records`
```sql
CREATE TABLE idempotency_records (
    idempotency_key   TEXT PRIMARY KEY,
    response_status   INT NOT NULL,
    response_body     TEXT NOT NULL,
    created_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```
- Stores the full serialized JSON response for each unique `idempotencyKey`
- On duplicate request: return `response_status` + `response_body` directly, no transfer logic executed

---

## Concurrency Strategy

**Approach: `SELECT FOR UPDATE` with consistent lock ordering**

When a transfer starts, we lock both wallet rows before reading or modifying anything:

```sql
SELECT * FROM wallets WHERE id IN (?, ?) ORDER BY id FOR UPDATE
```

The `ORDER BY id` ensures wallets are always locked in ascending numeric order, regardless of which is the source or destination. This prevents deadlocks when two transfers touch the same pair of wallets in opposite directions.

### Full transaction sequence

```
BEGIN
  1. SELECT wallets WHERE id IN (from, to) ORDER BY id FOR UPDATE
  2. INSERT INTO transfers (status = PENDING)          ← record exists before outcome
  3. Check from_wallet.balance >= amount
     → if insufficient:
          UPDATE transfers SET status = FAILED, failure_reason = 'INSUFFICIENT_FUNDS'
          COMMIT
          write 422 to idempotency_records
          return 422
  4. UPDATE wallets SET balance -= amount WHERE id = from_wallet
  5. UPDATE wallets SET balance += amount WHERE id = to_wallet
  6. INSERT INTO ledger_entries (DEBIT, from_wallet, amount)
  7. INSERT INTO ledger_entries (CREDIT, to_wallet, amount)
  8. UPDATE transfers SET status = PROCESSED
COMMIT
write 200 to idempotency_records
return 200
```

**Why INSERT transfer at step 2 (before balance check):**
The assignment requires the `PENDING → FAILED` transition to be observable. Inserting the transfer first ensures a FAILED row exists in the database for failed attempts — useful for audit and for demonstrating the state machine. Ledger entries are only created for `PROCESSED` transfers; a `FAILED` transfer has no ledger entries.

---

## Idempotency Strategy

**Approach: Separate `idempotency_records` table with full response caching**

### Flow

**Step 1 — Claim the idempotency key**

The very first thing on every request is to attempt an INSERT of a placeholder row:

```sql
INSERT INTO idempotency_records (idempotency_key, response_status, response_body)
VALUES (?, 0, 'PROCESSING')
ON CONFLICT DO NOTHING
```

- `response_status = 0` signals the record is claimed but not yet resolved
- **INSERT succeeds** → this request owns the key → proceed to execute the transfer
- **INSERT fails (conflict)** → another request already claimed the key → enter polling loop (see below)

**Step 2 — Execute transfer (only the owner does this)**

Execute the full transfer transaction. On any outcome (success or business failure), UPDATE the placeholder with the real response:

```sql
UPDATE idempotency_records
SET response_status = ?, response_body = ?
WHERE idempotency_key = ?
```

- Transfer succeeds → `response_status = 200`, body = transfer JSON
- Insufficient funds → `response_status = 422`, body = error JSON
- Wallet not found → `response_status = 404`, body = error JSON

Infrastructure errors (DB timeout, connection failure) are **not** stored — the placeholder stays as `PROCESSING` so the loser's poll loop eventually times out and the client can retry.

**Step 3 — Polling loop (for the request that lost the INSERT race)**

```
for attempt in 1..10:
    sleep(1 second)
    record = SELECT FROM idempotency_records WHERE idempotency_key = ?
    if record.response_status != 0:
        return record.response_status, record.response_body
return 503 Service Unavailable  // owner didn't finish within 10 seconds
```

10 retries × 1 second = 10 second wait maximum before giving up with a 503.

**Step 4 — Already-resolved duplicate (normal case)**

If the initial INSERT fails AND the existing record already has `response_status != 0`, skip the polling loop entirely and return immediately.

### Failed first request (e.g. insufficient funds)

If the first attempt returns 422, the `idempotency_records` row is updated with `response_status=422`. A second request 5 minutes later with the same key finds the resolved record immediately (Step 4 above) and returns the same 422 — even if the wallet now has funds.

The idempotency key represents a specific transfer attempt. To retry after fixing the issue, the caller must use a new key.

---

## Project Structure

```
wallet/src/main/java/com/kulu/wallet/
  WalletApplication.java
  controller/
    TransferController.java         -- POST /transfers
  service/
    TransferService.java            -- idempotency check, orchestration, business logic
  repository/
    WalletRepository.java           -- wallet reads/updates
    TransferRepository.java         -- transfer inserts/updates/reads
    LedgerRepository.java           -- ledger entry inserts
    IdempotencyRepository.java      -- idempotency record reads/writes
  domain/
    Wallet.java
    Transfer.java                   -- includes TransferStatus enum (PENDING, PROCESSED, FAILED)
    LedgerEntry.java                -- includes EntryType enum (DEBIT, CREDIT)
  dto/
    TransferRequest.java            -- API input
    TransferResponse.java           -- API output
  exception/
    InsufficientFundsException.java
    WalletNotFoundException.java
    GlobalExceptionHandler.java     -- maps exceptions to HTTP responses

wallet/src/main/resources/
  application.properties            -- DB connection config
  schema.sql                        -- DDL, auto-run on startup
```

---

## API Contract

### `POST /transfers`

**Request**
```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": 1,
  "toWalletId": 2,
  "amount": 100
}
```

**Response `200 OK`**
```json
{
  "transferId": "550e8400-e29b-41d4-a716-446655440000",
  "status": "PROCESSED",
  "fromWalletId": 1,
  "toWalletId": 2,
  "amount": 100
}
```

**Error responses**

| Code | Reason |
|---|---|
| `400` | Missing/invalid fields, amount ≤ 0, or `fromWalletId == toWalletId` |
| `404` | Wallet does not exist |
| `422` | Insufficient funds |

Duplicate `idempotencyKey` requests return the original status + body (200 or error).

### `GET /wallets/{id}/balance` *(optional)*
Returns current balance for a wallet.

### `GET /transfers/{id}` *(optional)*
Returns a transfer by ID — useful for polling transfer status.

---

## Testing Strategy

Follow Red → Blue → Green: write a failing test first, implement the minimum to pass, then refactor.

### Unit tests (`TransferServiceTest`)
Test service logic in isolation with a real (embedded/test) database:
- Successful transfer: balances updated, ledger has 2 entries, status = PROCESSED
- Insufficient funds: transfer status = FAILED, balances unchanged, no ledger entries
- Same-wallet transfer: rejected with 400 before any DB write
- Wallet not found: rejected with 404

### Integration tests (`TransferIntegrationTest`)
Test the full HTTP stack against a real PostgreSQL instance (use Testcontainers):
- POST /transfers happy path — 200 with correct response body
- POST /transfers idempotency — same key returns same response, only one transfer row exists
- POST /transfers duplicate key on failed transfer — second request returns same 422, no new transfer row
- POST /transfers concurrent duplicate — two threads fire simultaneously, one succeeds, other gets correct response via poll loop
- Ledger correctness — after a transfer, `SUM(DEBIT) == SUM(CREDIT)` for that transfer_id
- Concurrency safety — two concurrent transfers both debiting wallet_1; final balance must be exactly correct (no double-spend)

### Concurrency test
Spin up N threads (e.g. 10) all transferring from wallet_1 simultaneously. After all complete, assert:
- `wallet_1.balance == initial_balance - (N × amount)`
- `ledger_entries` count == 2 × number of successful transfers
- No negative balance

---

## Assumptions and Tradeoffs

| Decision | Assumption / Tradeoff |
|---|---|
| Stored balance over derived | Faster reads; requires atomic update alongside ledger — acceptable since both happen in the same transaction |
| `SELECT FOR UPDATE` over optimistic locking | Simpler, no retry logic in service layer; lower throughput under high contention — acceptable for this scope |
| Idempotency poll loop (10 × 1s) | Handles concurrent duplicate races cleanly; adds up to 10s latency in the extreme case — acceptable for a demo |
| FAILED transfer still creates a row | Demonstrates PENDING → FAILED state machine; creates a DB row per failed attempt — acceptable for an assignment |
| Wallets seeded via schema.sql | No wallet creation API; simplifies the scope to focus on transfers |
| Integer wallet IDs | Simpler than UUIDs; auto-incremented by PostgreSQL BIGSERIAL |

---

## What Is Deliberately Left Out

- No wallet creation API (wallets are seeded via `schema.sql` for testing)
- No authentication/authorization
- No pagination on any list endpoints
- No soft-delete or wallet deactivation
