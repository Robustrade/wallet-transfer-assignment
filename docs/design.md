# Wallet Transfer Service — Design Document

## 1. Overview & Requirements

The wallet transfer service handles money transfers between internal accounts while ensuring balance consistency, double-entry ledger correctness, request idempotency, and safe concurrent execution.

### Key Requirements
- `POST /transfers` endpoint to create a transfer using an `idempotencyKey`.
- Replaying the same `idempotencyKey` returns the original result without executing a second transfer.
- Every transfer produces exactly two ledger entries (one debit and one credit) of equal amount.
- Concurrent debit attempts on the same wallet cannot double-spend or produce a negative balance.
- Transfers running in opposite directions between the same two wallets must not cause database deadlocks.
- Transfers track lifecycle state: `PENDING`, `PROCESSED`, or `FAILED`.

### Scope & Assumptions
- Amounts are stored as positive 64-bit integers representing paise (e.g. 5000 = ₹50.00) to avoid floating-point rounding errors.
- Single base currency.
- PostgreSQL is used for persistence to leverage ACID transactions and row-level locking.

---

## 2. API Specification

### `POST /transfers`

Creates a transfer between two wallets.

#### Request Body
```json
{
  "idempotencyKey": "trans_123e4567-e89b-12d3-a456-426614174000",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 5000
}
```

#### Validation Rules
- `idempotencyKey`: string, required, non-empty.
- `fromWalletId`: string, required, non-empty.
- `toWalletId`: string, required, non-empty, must be different from `fromWalletId`.
- `amount`: integer, required, strictly greater than 0.

#### Responses
- `201 Created`: Transfer processed successfully. Returns the transfer details.
- `200 OK`: Duplicate request with matching payload. Returns the original response with header `X-Idempotent-Replay: true`.
- `400 Bad Request`: Invalid request payload (missing fields, negative amount, same source and destination).
- `409 Conflict`: Idempotency key reused with different transfer parameters.
- `422 Unprocessable Entity`: Business rule failure (e.g. source wallet has insufficient balance, or wallet does not exist).
  ```json
  {
    "error": "INSUFFICIENT_FUNDS",
    "message": "source wallet has insufficient balance"
  }
  ```

---

## 3. Database Schema

Schema integrity is enforced using primary keys, foreign keys, check constraints, and unique indexes.

### `wallets`
Stores the current balance for fast balance checks and row-level locking.
```sql
CREATE TABLE wallets (
    id          VARCHAR(64) PRIMARY KEY,
    balance     BIGINT NOT NULL CHECK (balance >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```
The check constraint `balance >= 0` ensures that a wallet balance can never drop below zero at the database level.

### `transfers`
Stores transfer records and their current lifecycle state.
```sql
CREATE TABLE transfers (
    id              VARCHAR(64) PRIMARY KEY,
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    from_wallet_id  VARCHAR(64) NOT NULL REFERENCES wallets(id),
    to_wallet_id    VARCHAR(64) NOT NULL REFERENCES wallets(id),
    amount          BIGINT NOT NULL CHECK (amount > 0),
    status          VARCHAR(20) NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    error_reason    TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT check_different_wallets CHECK (from_wallet_id <> to_wallet_id)
);
```

### `ledger_entries`
The double-entry ledger. Rows in this table are append-only and cannot be updated or deleted.
```sql
CREATE TABLE ledger_entries (
    id          VARCHAR(64) PRIMARY KEY,
    transfer_id VARCHAR(64) NOT NULL REFERENCES transfers(id) ON DELETE RESTRICT,
    wallet_id   VARCHAR(64) NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    type        VARCHAR(10) NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ledger_transfer_id ON ledger_entries(transfer_id);
CREATE INDEX idx_ledger_wallet_id ON ledger_entries(wallet_id);
```

### `idempotency_records`
Stores the original HTTP response code and body for duplicate request replay.
```sql
CREATE TABLE idempotency_records (
    key             VARCHAR(255) PRIMARY KEY,
    request_hash    VARCHAR(64) NOT NULL,
    response_code   INT NOT NULL,
    response_body   TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## 4. Concurrency & Locking Strategy

### Preventing Deadlocks
Deadlocks can occur when two transfers attempt to lock the same pair of wallets in reverse order at the same time:
- Transfer 1 locks Wallet A, waits for Wallet B.
- Transfer 2 locks Wallet B, waits for Wallet A.

To avoid this, row locks on `wallets` are always acquired in deterministic order (alphabetically sorted by wallet ID):
```sql
SELECT id, balance 
FROM wallets 
WHERE id IN ($1, $2) 
ORDER BY id 
FOR UPDATE;
```
Because both transactions lock the smaller ID first followed by the larger ID, circular wait conditions cannot occur.

### Preventing Double Spending
1. Inside a single database transaction, `SELECT ... FOR UPDATE` locks the wallet rows.
2. Concurrent transfers attempting to debit the same wallet block until the first transaction commits or rolls back.
3. Once the lock is acquired, the service reads the committed balance.
4. If `balance < amount`, the transaction marks the transfer as `FAILED` and exits with `422 Unprocessable Entity`.
5. If funds are sufficient:
   - Source wallet balance is decremented: `balance = balance - amount`.
   - Destination wallet balance is incremented: `balance = balance + amount`.
   - Transfer record is updated to `PROCESSED`.
   - Two ledger rows are inserted (DEBIT for source, CREDIT for destination).
   - Idempotency record is inserted.
   - Transaction commits.

---

## 5. Idempotency Strategy

### Duplicate Detection
1. The service computes a SHA-256 hash of the canonical request payload: `SHA256(fromWalletId + "|" + toWalletId + "|" + amount)`.
2. It queries `idempotency_records` by `key`.
3. If the record exists:
   - If the stored `request_hash` matches the incoming payload hash, the cached response code and body are returned with header `X-Idempotent-Replay: true`.
   - If the hash differs, the service returns `409 Conflict` (idempotency key reused with different parameters).
4. If the record does not exist:
   - The transfer is executed within a database transaction.
   - The transfer record, ledger rows, and idempotency record are written in the same transaction.

### Handling Concurrent Duplicate Requests
If two identical requests with the same `idempotencyKey` arrive simultaneously:
- Both requests miss the initial lookup.
- Both attempt to execute and insert into `idempotency_records`.
- PostgreSQL's primary key constraint on `idempotency_records(key)` allows only one transaction to commit.
- The losing transaction receives error code `23505` (`unique_violation`), rolls back cleanly without applying side effects, queries the winner's committed record, and returns that cached response.

---

## 6. Transfer State Machine

Allowed states are `PENDING`, `PROCESSED`, and `FAILED`.

Lifecycle:
- Transfers start in `PENDING`.
- If balance is sufficient and accounts are valid, the status transitions to `PROCESSED`.
- If balance is insufficient or an account is invalid, the status transitions to `FAILED` with an error reason.

Terminal states (`PROCESSED`, `FAILED`) are immutable. Guarded SQL updates enforce that transitions can only occur from `PENDING`:
```sql
UPDATE transfers 
SET status = 'PROCESSED', updated_at = NOW() 
WHERE id = $1 AND status = 'PENDING';
```
If the status is not `PENDING`, zero rows are updated.

---

## 7. Project Architecture

The codebase follows a standard layered architecture:

- `internal/domain`: Pure entities (`Wallet`, `Transfer`, `LedgerEntry`), error definitions, and validation logic.
- `internal/repository`: Database access using `sqlc` and transaction management with `pgxpool`.
- `internal/service`: Business logic, idempotency orchestration, and balance checks.
- `internal/handler`: HTTP request parsing, input validation, and response mapping.
- `cmd/api`: Application bootstrap, configuration, and HTTP server lifecycle.

---

## 8. Testing Strategy

The test suite validates behavior directly against the database:

1. **Unit Tests**:
   - Request payload validation (missing fields, negative amounts, self-transfers).
   - Domain state transitions.
2. **Integration Tests**:
   - Successful transfer updates both balances and creates two balanced ledger entries.
   - Idempotent replay returns the cached response without creating duplicate ledger rows or balance changes.
   - Idempotency key reuse with different parameters returns `409 Conflict`.
   - Insufficient funds returns `422`, saves transfer as `FAILED`, and leaves balances unchanged.
3. **Concurrency Tests**:
   - 50 concurrent goroutines attempting to debit from a single wallet with limited funds to verify that no double-spending occurs and balances never drop below zero.
   - Bidirectional concurrent transfers between two wallets to verify deadlock-free execution.
