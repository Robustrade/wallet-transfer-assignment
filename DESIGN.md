
Hi team, before implementing the complete solution, here is an outline of my understanding and proposed approach to solve the wallet transfer assignment.

#### 1. Core Understanding

The goal is to build a highly reliable, concurrent wallet-to-wallet transfer Service. 
The primary engineering challenges are guaranteeing exactly-once processing (idempotency), strictly maintaining a balanced double-entry ledger, and preventing race conditions/deadlocks during concurrent requests.

#### 2. Proposed Architecture & Stack

* **Stack:** Go, Gin (for HTTP routing), and PostgreSQL.
* **Architecture:** Clean, layered separation (Handler $\rightarrow$ Service $\rightarrow$ Repository $\rightarrow$ Domain).
* To maintain a clean separation of concerns, the service will be structured using a clean, layered architecture: Handler Layer (HTTP routing/validation) $\rightarrow$ Service Layer (orchestration/business rules) $\rightarrow$ Repository Layer (SQL queries/database transactions) $\rightarrow$ Domain Models (core entities/state transitions).

#### 3. Database Schema (PostgreSQL)

I will use 4 primary tables.

* `idempotency_records`: `idempotency_key`(PRIMARY KEY), `request_hash`, `response_status`, `response_body`, `created_at`(TIMESTAMP with TIME ZONE DEFAULT CURRENT_TIMESTAMP).
* `wallets`: `id`, `balance` (with a `CHECK balance >= 0` constraint).
* `transfers`: `id`, `idempotency_key` (UNIQUE index), `from_wallet_id`, `to_wallet_id`, `amount`, `status`.
* `ledger_entries`: `id`, `transfer_id`, `wallet_id`, `type` (DEBIT/CREDIT), `amount`.

*(Note: To favor simplicity and prevent orphaned states in the event of an application crash, I am executing all the operations on the tables and handling everything within a single transaction).*

#### 4. Concurrency & Idempotency Strategy

I will rely on PostgreSQL's native ACID properties to handle distributed system edge cases:

* **Idempotency:** A `UNIQUE` constraint on `idempotency_records.idempotency_key`. If a concurrent retry hits the DB, the uncommitted lock will briefly block it, then safely reject it once the first transaction completes, allowing the API to return the processed response/status.
* **Concurrency (Race Conditions):** The entire transfer lifecycle will be wrapped in a single database transaction. I will use pessimistic locking (`SELECT ... FOR UPDATE`) on the wallet rows to ensure sequential balance deductions.
* **Deadlock Prevention:** To prevent A $\rightarrow$ B and B $\rightarrow$ A deadlocks, the application will consistently sort the wallet UUIDs lexicographically(fromWalletId, then toWalletId) before acquiring the row locks.


#### 5. Design Decisions & Trade-offs
##### Chosen Approach: Single Atomic Transaction
* **Pros**: Simplicity. Zero possibility of orphaned states (such as writing an idempotency key as PENDING but crashing before money moves, locking the user out indefinitely). Relies directly on the maturity of PostgreSQL ACID mechanics.

* **Cons** : Concurrent retries will briefly hang/block at the database level while waiting for the original transaction to commit, rather than immediately returning a 409 Conflict. Given a targeted REST API response time of $<50\text{ms}$, this brief block is a preferred trade-off over implementing asynchronous sweepers and cleanup crons.


#### 6. Scaling(Not Going to implement but design constraints)
* Consider implementing patroni cluster for better node management
* consider Implementing DB wrapper such that underlying application interacts with the DB wrapper and need not be aware of the underlying DB, such that we can swap out for mySQL, Oracle DB if required later.
* Consider implementing the logic to maintain the connection pool.
* consider implementing additional services/crontabs and additiona of Queues to retry/clearn  any dangling requests.