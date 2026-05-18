# Approach & Implementation Details

This document outlines the design decisions, technical strategies, and architectural patterns used to implement the Wallet Transfer Service. The solution was built using **Golang** and **PostgreSQL** to ensure high concurrency safety, strict idempotency, and a robust double-entry ledger.

## 1. Database Schema Design
The system relies heavily on PostgreSQL to maintain state correctly. 

- `wallets`: Stores the current balance for each wallet. 
- `transfers`: Tracks the lifecycle of a transfer request (PENDING, PROCESSED, FAILED). This table includes a `UNIQUE` constraint on `idempotency_key` to serve as a secondary guard against duplicate inserts.
- `ledger_entries`: Represents the immutable double-entry ledger. Every successful transfer inserts exactly two rows here (one DEBIT, one CREDIT) tied to the respective wallets.
- `idempotency_keys`: A dedicated table used strictly to lock incoming requests and cache their final HTTP responses. 

*Indexes were added to all foreign key columns (like `wallet_id` and `transfer_id`) to optimize join and lookup performance.*

## 2. Concurrency Handling & Transaction Strategy
Ensuring safety against race conditions (like double spending) is managed directly via the database rather than application-level locks, allowing the application to safely scale horizontally across multiple instances.

- **Row-Level Locking**: Inside a SQL transaction, the system acquires an exclusive lock on the source and destination wallets using `SELECT ... FOR UPDATE`.
- **Deadlock Prevention**: To prevent deadlocks when two concurrent transactions attempt to lock the exact same pair of wallets but in reverse order, the application **always sorts the wallet IDs** lexicographically before acquiring the locks.
- **Foreign Key Shared Lock Contention**: The `FOR UPDATE` lock on wallets is intentionally acquired *before* inserting into the `transfers` table. This prevents PostgreSQL from implicitly acquiring a shared `FOR KEY SHARE` lock which would otherwise cause heavy deadlock contention during high load.
- **Atomicity**: The balance deduction, balance addition, transfer state update, and ledger entry insertions all happen within a single ACID transaction. If any step fails (e.g., insufficient funds), the transaction rolls back cleanly.

## 3. Idempotency Strategy
The system provides exactly-once semantics using the `idempotency_keys` table.

1. **Lock Acquisition**: When a request arrives, the system attempts to `INSERT INTO idempotency_keys ... ON CONFLICT DO NOTHING`.
2. **Conflict Resolution**: If the insert fails, it means the key already exists.
    - If the existing key has a `NULL` response code, it means another request is *currently processing* it. The system immediately rejects the duplicate with a `409 Conflict`.
    - If the existing key has a saved response, the system bypasses the transfer logic entirely and successfully returns the cached response.
3. **Commit Phase**: After the transfer succeeds (or fails gracefully), the HTTP status code and JSON response body are `UPDATE`d onto the idempotency key record for future replays.

## 4. Architecture & Code Structure
The codebase follows a Clean Architecture pattern, heavily decoupling concerns:
- **`model`**: Contains core structs and API request payloads.
- **`repository`**: Encapsulates all PostgreSQL driver (`pgx`) interactions, transaction boundaries, and raw SQL queries.
- **`service`**: Holds the business logic, orchestrates the idempotency checks, and prepares the HTTP response structures.
- **`handler`**: A thin HTTP transport layer (using `go-chi`) responsible only for routing, parsing JSON bodies, and writing out HTTP responses.

## 5. Assumptions & Tradeoffs
- **PostgreSQL Dependency**: The solution firmly couples itself to PostgreSQL's specific locking behaviors (`FOR UPDATE`) and concurrency models. While this makes migrating to a NoSQL database difficult, it provides the most mathematically sound safety guarantees for a financial ledger.
- **Balance Normalization**: The wallet balance is kept denormalized in the `wallets` table rather than dynamically calculated by summing the `ledger_entries` on every request. This is a tradeoff favoring extremely fast read performance and straightforward locking over pure ledger normalization.
- **Fail-Fast Error Handling**: If a completely unexpected internal server error occurs during processing, we do *not* cache the 500 error in the idempotency table. Instead, we allow the idempotency lock to remain or be cleared so the client can safely retry the request later. 
