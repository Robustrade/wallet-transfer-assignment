# Wallet Transfer Service

A Go service implementing wallet-to-wallet transfers with:

* Idempotent request handling
* Transactional balance updates
* Double-entry ledger recording
* PostgreSQL persistence
* Concurrent transfer safety
* Deadlock avoidance
* Safe transfer state transitions
* Behavioral and concurrency tests

## Architecture

The service follows a layered architecture:

```text
HTTP Handler
     |
     v
Transfer Service
     |
     +------------------+
     |                  |
     v                  v
Transfer Repository   Wallet Repository
     |
     v
Ledger Repository
     |
     v
PostgreSQL
```

### Handler

Responsible for:

* HTTP request decoding
* Basic request validation
* HTTP status mapping
* Calling the transfer service

The handler does not contain database or transfer business logic.

### Service

Responsible for:

* Transfer validation
* Idempotency behavior
* Transaction orchestration
* Balance validation
* Wallet locking
* Ledger creation
* Transfer state transitions

### Repository

Responsible for PostgreSQL persistence operations.

Repositories receive the transaction controlled by the service so that all transfer-related database operations participate in the same transaction.

### Domain

Contains wallet, transfer, and ledger entities and transfer/ledger status definitions.

---

## Database Design

The service uses three tables:

### `wallets`

Stores the current wallet balance.

```text
id
balance
created_at
```

The balance has a database constraint preventing negative values.

### `transfers`

Stores the transfer request and its state.

```text
id
idempotency_key
from_wallet_id
to_wallet_id
amount
status
request_hash
created_at
updated_at
```

`idempotency_key` has a unique constraint.

The transfer status is constrained to:

```text
PENDING
PROCESSED
FAILED
```

### `ledger_entries`

Stores the double-entry ledger records.

Each successful transfer creates:

```text
DEBIT  source wallet
CREDIT destination wallet
```

Both entries reference the same transfer.

---

## Transfer Transaction

A successful transfer is performed atomically in a single PostgreSQL transaction:

```text
BEGIN
  |
  +-- Check idempotency key
  |
  +-- Create PENDING transfer
  |
  +-- Lock source and destination wallets
  |
  +-- Validate source balance
  |
  +-- Debit source wallet
  |
  +-- Credit destination wallet
  |
  +-- Create DEBIT ledger entry
  |
  +-- Create CREDIT ledger entry
  |
  +-- Mark transfer PROCESSED
  |
COMMIT
```

If any operation fails, the transaction is rolled back.

---

## Idempotency

The `idempotency_key` is stored durably in the `transfers` table and protected by a unique database constraint.

A request hash is also stored for the transfer.

The request hash is calculated from:

```text
fromWalletId
toWalletId
amount
```

For a repeated request:

```text
same idempotency key
+
same request parameters
```

the existing transfer is returned and no new side effects are performed.

If the same idempotency key is reused with different request parameters, the service returns an idempotency conflict.

The database unique constraint acts as the concurrency arbiter when multiple requests with the same idempotency key arrive simultaneously.

This makes duplicate requests safe across application retries and process restarts because the idempotency state is stored in PostgreSQL rather than in application memory.

---

## Transfer States

Transfers use the following state machine:

```text
             +-------------+
             |   PENDING   |
             +------+------+
                    |
             +------+------+
             |             |
             v             v
      +-------------+ +-------------+
      |  PROCESSED  | |    FAILED   |
      +-------------+ +-------------+
```

Allowed transitions:

```text
PENDING -> PROCESSED
PENDING -> FAILED
```

A processed or failed transfer is not executed again when the same idempotency key is retried.

Insufficient balance results in a persisted `FAILED` transfer.

---

## Concurrency Strategy

Concurrent transfers use PostgreSQL row-level locking with:

```sql
SELECT ...
FOR UPDATE
```

Both the source and destination wallet rows are locked before balances are modified.

To avoid deadlocks, the two wallet IDs are sorted and locked in deterministic order.

For example:

```text
Transfer A -> B
Transfer B -> A
```

Both transactions acquire locks in the same wallet-ID order.

This prevents a circular lock dependency.

### Double-spend prevention

Consider:

```text
Wallet A = 100

Transfer 1: A -> B = 80
Transfer 2: A -> C = 80
```

Both transactions cannot successfully spend the same 100.

The first transaction that obtains the wallet lock can update the balance. The second transaction observes the updated balance and fails due to insufficient funds.

---

## Ledger Consistency

A successful transfer always creates exactly two ledger entries:

```text
DEBIT  source      amount
CREDIT destination amount
```

Both entries are created in the same database transaction as the balance updates.

Therefore a partial transfer cannot commit only one side of the ledger.

For example:

```text
Before:

A = 100
B = 50

Transfer A -> B = 25

After:

A = 75
B = 75
```

Ledger:

```text
A  DEBIT  25
B  CREDIT 25
```

---

## Failure Handling

Insufficient balance follows:

```text
PENDING
   |
   v
FAILED
```

The failed state is committed so that retries can safely return the original failed result.

No wallet balance is modified and no ledger entries are created for an insufficient-balance transfer.

---

## API

### Create Transfer

```text
POST /transfers
```

Example:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet-id-1",
  "toWalletId": "wallet-id-2",
  "amount": 100
}
```

A successful transfer returns the created transfer.

The service also exposes:

```text
GET /health
```

which returns:

```text
OK
```

---

## Running Locally

### Requirements

* Go
* Docker
* Docker Compose
* PostgreSQL client (`psql`) is useful for local database inspection

### Start PostgreSQL

```bash
docker compose up -d
```

The repository's Docker configuration exposes PostgreSQL on host port `5433`:

```text
localhost:5433 -> PostgreSQL:5432
```

### Apply the migration

```bash
docker exec -i wallet-transfer-postgres \
  psql -U wallet -d wallet_transfer \
  < migrations/001_init.sql
```

### Run the service

```bash
DATABASE_URL="postgres://wallet:wallet@127.0.0.1:5433/wallet_transfer" \
go run ./cmd/server
```

The server listens on:

```text
http://localhost:8080
```

### Health check

```bash
curl http://localhost:8080/health
```

Expected:

```text
OK
```

---

## Testing

Run all tests:

```bash
DATABASE_URL="postgres://wallet:wallet@127.0.0.1:5433/wallet_transfer" \
go test ./...
```

Run with the Go race detector:

```bash
DATABASE_URL="postgres://wallet:wallet@127.0.0.1:5433/wallet_transfer" \
go test -race ./...
```

The test suite covers:

* Successful transfers
* Insufficient balance
* Persisted failed transfers
* Failed transfer idempotency
* Idempotency conflicts
* Concurrent debits
* Concurrent duplicate requests
* Concurrent reverse transfers
* Ledger entry counts
* Balance conservation
* Deadlock avoidance

Concurrency scenarios were also executed repeatedly with `-count` to increase confidence in the locking and idempotency behavior.

---

## Design Tradeoffs

### Stored balance vs derived balance

The implementation maintains a stored wallet balance instead of calculating balances from the ledger on every request.

This keeps balance reads and transfer validation simple and efficient.

Correctness is maintained because balance updates and ledger writes occur in the same database transaction.

### PostgreSQL row locks

Pessimistic row-level locking was selected because wallet transfers are naturally represented as updates to a small number of wallet rows.

`FOR UPDATE` makes concurrent balance modifications straightforward to reason about.

### Durable idempotency

Idempotency state is stored with the transfer rather than in an in-memory cache.

This allows idempotency behavior to survive application restarts.

---

## Verification

The implementation was verified with:

```text
go test ./...
go test -race ./...
```

Both complete suites pass against PostgreSQL.

Concurrency-specific tests were additionally executed repeatedly using `-count`.

---

## AI Usage

AI assistance was used during development for:

* Understanding the assignment requirements
* Discussing transaction and concurrency design
* Reviewing implementation decisions
* Designing behavioral and concurrency tests
* Diagnosing test failures
* Reviewing the implementation against the evaluation rubric
* Improving documentation

The final implementation was reviewed and executed locally, and the developer verified the resulting behavior through the test suite and PostgreSQL integration tests.

See `AI_USAGE.md` for the detailed AI usage record.
