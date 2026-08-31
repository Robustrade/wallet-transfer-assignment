# Wallet Transfer Service

## Overview

Wallet Transfer Service is a Go backend service that supports safe wallet-to-wallet transfers.

The application is designed using Clean Architecture and PostgreSQL. The main focus of the implementation is financial correctness, idempotency, transactional processing, double-entry ledger recording, and concurrency safety.

## Technology Stack

- Go
- Gin HTTP framework
- PostgreSQL
- pgx / pgxpool for PostgreSQL connectivity
- Docker and Docker Compose
- UUID for transfer IDs
- PostgreSQL transactions and row-level locking
- Go testing package for integration/behavioral tests

## Architecture

The application follows a Clean Architecture style with separation between:

- Domain
- Service
- Repository
- Handler
- Database
- Server/bootstrap

The dependency flow is designed so that the HTTP handler calls the service, and the service uses the repository abstraction.

The application is wired using dependency injection in `main.go`:

```text
PostgreSQL connection
        ↓
PostgresTransferRepository
        ↓
TransferService
        ↓
TransferHandler
        ↓
Gin Router
```

This keeps the business logic separate from HTTP and database implementation details.

## Main API

### Create Transfer

```http
POST /transfers
```

Example request:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

The service validates the request and processes the transfer atomically.

## Idempotency

The transfer API uses an idempotency key.

The `idempotency_key` column has a UNIQUE constraint in PostgreSQL.

When a request with an existing idempotency key is received, the existing transfer is returned instead of creating another transfer.

This prevents duplicate transfers when the client retries the same request.

The implementation also checks that the request details match the original request. Reusing the same idempotency key with different wallet IDs or amount results in an idempotency conflict.

At the API level, a successfully processed request with the same idempotency key returns the original transfer result.

## Transfer State Machine

Transfers have three states:

```text
PENDING
   │
   ├──→ PROCESSED
   │
   └──→ FAILED
```

A transfer is initially created as `PENDING`.

If the source wallet has sufficient balance, the transfer is processed and the state becomes `PROCESSED`.

If there is insufficient balance, the transfer becomes `FAILED`.

The transfer status and response are stored in PostgreSQL.

## Wallet Balance Strategy

This application uses a stored wallet balance.

The `wallets` table contains a `balance` column.

During a transfer:

1. The source wallet is locked.
2. The destination wallet is locked.
3. The source balance is checked.
4. The source balance is decreased.
5. The destination balance is increased.
6. Ledger entries are created.
7. The transfer is marked as processed.
8. The transaction is committed.

The balance and ledger changes are part of the same PostgreSQL transaction.

## Concurrency Safety

The application uses PostgreSQL transactions together with row-level locking.

Wallets are locked using:

```sql
SELECT id, balance
FROM wallets
WHERE id = $1
FOR UPDATE;
```

`FOR UPDATE` prevents two concurrent transfers from modifying the same wallet balance at the same time.

For example, if two transfers try to spend money from the same wallet:

```text
Wallet balance = 1000

Transfer A = 700
Transfer B = 700
```

Only one transfer can successfully use the available balance.

The second transaction waits for the row lock and then sees the updated balance. It therefore fails because only 300 remains.

The service also locks both wallets in deterministic sorted order. This helps prevent deadlocks when two concurrent transfers involve the same pair of wallets in opposite directions.

## Database Transactions

A transfer is executed inside one PostgreSQL transaction.

Conceptually:

```text
BEGIN
  ↓
Check idempotency
  ↓
Create PENDING transfer
  ↓
Lock source wallet
  ↓
Lock destination wallet
  ↓
Check balance
  ↓
Update source balance
  ↓
Update destination balance
  ↓
Create DEBIT ledger entry
  ↓
Create CREDIT ledger entry
  ↓
Mark transfer PROCESSED
  ↓
COMMIT
```

If an error occurs before commit, the transaction is rolled back.

This prevents partial transfers such as:

```text
source debited
destination not credited
```

or:

```text
balance updated
ledger entry missing
```

## Double-Entry Ledger

Every successful transfer creates exactly two ledger entries.

Example:

```text
entry_id | wallet_id | transfer_id | type   | amount
---------+-----------+-------------+--------+-------
1        | wallet_1  | T1          | DEBIT  | 100
2        | wallet_2  | T1          | CREDIT | 100
```

The source wallet receives a `DEBIT` entry.

The destination wallet receives a `CREDIT` entry.

Both entries are created inside the same database transaction as the balance updates.

Therefore, either the complete transfer and both ledger entries are committed, or the transaction is rolled back.

## Database Tables

The main tables are:

### wallets

Stores the current wallet balance.

Important fields include:

- `id`
- `balance`
- timestamps

### transfers

Stores transfer requests and their state.

Important fields include:

- `id`
- `idempotency_key`
- `from_wallet_id`
- `to_wallet_id`
- `amount`
- `status`
- `response`
- timestamps

The `idempotency_key` is unique.

### ledger_entries

Stores the double-entry accounting records.

Important fields include:

- `id`
- `wallet_id`
- `transfer_id`
- `type`
- `amount`
- timestamp

The application does not require a separate `idempotency_records` table because idempotency information is stored directly in the `transfers` table.

## Repository Layer

The repository defines the database operations required by the service.

Examples include:

```text
BeginTx
GetTransferByIdempotencyKey
CreateTransfer
LockWallet
UpdateWalletBalance
CreateLedgerEntry
UpdateTransferStatus
```

The repository interface separates business logic from PostgreSQL implementation.

`PostgresTransferRepository` implements the interface using `pgxpool` and `pgx.Tx`.

## Service Layer

The service contains the transfer business rules.

The main method is:

```text
CreateTransfer
```

It is responsible for:

- validating transfer input
- checking idempotency
- creating the transfer
- locking wallets
- checking sufficient balance
- updating balances
- creating ledger entries
- changing transfer status
- committing the transaction

The service does not directly execute SQL. It works through the repository abstraction.

## Handler Layer

The Gin handler receives HTTP requests and converts them into service input.

The request contains:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Gin binding/validation is used for required fields and positive amounts.

The handler returns the transfer response as JSON.

## Docker

The application is containerized using Docker.

Docker Compose runs:

```text
wallet-transfer-service
        ↓
PostgreSQL
```

The PostgreSQL container exposes its internal port 5432 to the host on port 5437.

Example:

```yaml
ports:
  - "5437:5432"
```

Inside the Docker Compose network, the Go service connects to PostgreSQL using the service name:

```text
postgres:5432
```

The host port `5437` is used by tools such as pgAdmin running on the host machine.

## Database Initialization

The database schema is maintained in:

```text
migrations/001_init.sql
```

When PostgreSQL initializes a new data directory, the SQL file can be mounted into:

```text
/docker-entrypoint-initdb.d/
```

PostgreSQL executes initialization scripts when the database data directory is created for the first time.

## Testing

The project includes integration/behavioral tests using a real PostgreSQL database.

Tests cover:

- transfer execution
- idempotency
- idempotency conflict
- insufficient balance
- wallet-not-found scenarios
- ledger correctness
- transaction rollback
- row-level locking
- concurrent transfers
- repository operations

The tests verify behavior through PostgreSQL rather than testing only implementation details.

Separate test databases are recommended for service and repository tests so that tests cannot interfere with each other through `TRUNCATE` operations.

## Concurrency Test

A concurrency test can start two transfers at the same time against the same source wallet.

For example:

```text
Initial wallet_1 = 1000

Transfer A:
wallet_1 → wallet_2 = 700

Transfer B:
wallet_1 → wallet_3 = 700
```

Because the available balance is only 1000, exactly one transfer can succeed.

The final source balance must be:

```text
300
```

This demonstrates that row-level locking prevents double spending.

## Error Handling

Important service errors include:

```text
insufficient wallet balance
wallet not found
idempotency key already used with different request
```

The service also handles PostgreSQL errors and transaction failures.

## Clean Architecture Benefits Used

The architecture provides:

### Separation of concerns

HTTP handling, business logic, and database operations are separated.

### Dependency inversion

The service depends on the repository interface rather than directly depending on PostgreSQL implementation details.

### Dependency injection

Dependencies are constructed and injected from the application entry point.

Example:

```text
db
 ↓
repository
 ↓
service
 ↓
handler
```

### Testability

The service can be tested independently from the HTTP layer, while repository behavior can be tested against PostgreSQL.

## Key Design Decisions

### Stored balances

The application maintains wallet balances directly in the `wallets` table instead of calculating the current balance from all ledger entries on every request.

### PostgreSQL transactions

All balance and ledger modifications belonging to one transfer are performed in a single transaction.

### Row-level locking

`SELECT ... FOR UPDATE` is used to serialize concurrent modifications to wallet balances.

### Deterministic lock ordering

Wallet IDs are sorted before locking to reduce the possibility of deadlocks.

### Idempotency stored with transfer

A separate `idempotency_records` table is not necessary for the current design because the idempotency key and original response are stored in `transfers`.

## Overall Flow

```text
HTTP Client
    │
    │ POST /transfers
    ▼
Gin Handler
    │
    ▼
Transfer Service
    │
    ├── Check idempotency
    │
    ├── BEGIN transaction
    │
    ├── Create PENDING transfer
    │
    ├── Lock wallets
    │
    ├── Check balance
    │
    ├── Update wallet balances
    │
    ├── Create DEBIT ledger entry
    │
    ├── Create CREDIT ledger entry
    │
    ├── Mark PROCESSED
    │
    └── COMMIT
    │
    ▼
PostgreSQL
```

## Summary

The Wallet Transfer Service is a transactional Go service designed to provide safe wallet transfers.

The key guarantees are:

- idempotent API requests
- no duplicate transfer for the same idempotency key
- stored wallet balances
- double-entry ledger records
- atomic transfer processing
- PostgreSQL row-level locking
- protection against concurrent double spending
- transaction rollback on failure
- explicit transfer states
- Clean Architecture
- dependency injection
- Dockerized application and PostgreSQL
- behavioral/integration testing
