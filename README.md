# Wallet Transfer Service

A robust wallet transfer service in Go adhering to Clean Architecture principles. The service provides exactly-once API semantics, maintains a strict double-entry ledger, and guarantees ACID compliance and deadlock-prevention under high concurrency using PostgreSQL (and an alternative SQLite implementation). 

## Architecture & Schema Design
The persistence layer is normalized and relies heavily on database-level constraints to prevent invalid states:
- **`wallets`**: Contains `id` and `balance`. Includes a `CHECK (balance >= 0)` constraint to prevent negative balances at the lowest level.
- **`transfers`**: Tracks the transfer request. Crucially includes a `UNIQUE NOT NULL` constraint on `idempotency_key`. It uses Foreign Keys linking to the `wallets` table.
- **`ledger_entries`**: Maintains the double-entry ledger. Every transfer strictly results in two entries (DEBIT and CREDIT), protected by Foreign Keys linking back to `wallets` and `transfers`.

## Idempotency Strategy
Idempotency is guaranteed via a mix of application logic and database constraints to handle retries safely:
1. **Service Check**: The service explicitly checks `GetTransferByIdempotencyKey` before execution. If found, it returns the existing record without side effects.
2. **Database Constraint**: If two identical requests hit the service at the exact same millisecond bypassing the initial check, the `UNIQUE` constraint on the `transfers.idempotency_key` column blocks the second insert. 
3. **Graceful Fallback**: The repository intercepts this constraint failure, returning a custom `ErrDuplicateTransfer`, which the service catches to fetch and return the successful parallel record.

## Concurrency Strategy
Race conditions and double spending are prevented entirely at the database layer using strict transactional boundaries:
- The entire transfer (balance checks, balance updates, and ledger entry inserts) is wrapped in a single transaction. If any step fails, everything is rolled back.
- **Read-then-Write Race Prevention:** In PostgreSQL, we use `SELECT ... FOR UPDATE` to exclusively lock the wallet rows during the balance check.
- **Deadlock Prevention:** Before locking, the service sorts the two Wallet IDs lexicographically. Locks are always acquired in this deterministic order, ensuring that concurrent bi-directional transfers between the same two wallets can never cause a database deadlock.

## How to Run
```bash
go run cmd/server/main.go
```

## How to Test
To run all tests including PostgreSQL integration tests, start a Postgres container and pass the connection string:
```bash
docker run --name wallet-postgres -e POSTGRES_USER=user -e POSTGRES_PASSWORD=password -e POSTGRES_DB=walletdb -p 5434:5432 -d postgres:15

# On Windows PowerShell:
$env:TEST_DB_URL="postgres://user:password@localhost:5434/walletdb?sslmode=disable"
go test ./... -v -cover
```
