# Wallet Transfer Service Design

## Scope

Implement a small HTTP service for wallet-to-wallet transfers. The service uses Go's standard library for HTTP and application code, `database/sql` with a PostgreSQL driver for persistence, and PostgreSQL transactions for atomic state changes. No HTTP framework is used.

## API contract

`POST /transfers` accepts JSON with `idempotencyKey`, `fromWalletId`, `toWalletId`, and `amount`. `amount` is a positive integer in the smallest supported currency unit; this avoids floating-point rounding. The initial version supports one currency and assumes wallet rows are provisioned separately.

A successful request returns the transfer identifier, `PROCESSED` state, wallet IDs, amount, and resulting balances. Invalid JSON or fields return `400`; an unknown wallet returns `404`; insufficient funds returns `409`; reusing an idempotency key with different request data returns `409`; unexpected persistence failures return `500`. Self-transfers are rejected. Responses use JSON and stable error codes/messages suitable for clients.

## Persistence model

- `wallets`: wallet ID text primary key, nonnegative balance in minor units, and creation timestamp.
- `transfers`: UUID primary key, source and destination wallet foreign keys, positive amount, state (`PENDING`, `PROCESSED`, or `FAILED`), optional failure code, and creation/completion timestamps. Self-transfers are prohibited.
- `ledger_entries`: identity primary key, transfer and wallet foreign keys, entry type (`DEBIT` or `CREDIT`), positive amount, and creation timestamp. A unique constraint on `(transfer_id, entry_type)` permits at most one debit and one credit per transfer.
- `idempotency_records`: idempotency key text primary key, SHA-256 request hash, state (`IN_PROGRESS` or `COMPLETED`), optional transfer foreign key, HTTP status, JSON response body, and timestamps. A completed record always contains its saved response; it refers to a transfer when one was created.

Indexes support transfer and ledger history lookups; foreign keys protect referential integrity. Nonnegative wallet balances, positive amounts, allowed states, and unique ledger entry types are enforced in PostgreSQL. A successful transfer writes exactly one debit and one credit in the same transaction as balance changes and the completed idempotency result. The service and behavioral tests verify that processed transfers have matching entries for the source and destination; the transaction prevents partial commits. Failed transfers are recorded for idempotent business-failure responses but do not change balances or create ledger entries.

## Idempotency and retries

The idempotency key is unique in PostgreSQL. Hash the canonical validated request fields (source wallet, destination wallet, and amount) with SHA-256 and store the hash with the result. Within the transfer transaction, insert an `IN_PROGRESS` record. If another transaction owns the key, PostgreSQL's unique constraint serializes the contenders; read the committed record and return its original saved HTTP status and JSON response when the request hash matches. Return `409` if the same key is presented with different request data.

Persist terminal outcomes for deterministic business failures such as unknown wallets and insufficient funds so a retry with the same key returns the original outcome. A failure detected before a transfer row exists can still be saved in the idempotency record without a transfer reference. Transient infrastructure failures roll back the whole transaction, including the key claim, allowing a later retry to attempt the operation again. The idempotency claim, transfer, ledger entries, balance updates, and stored response share one transaction, so a process crash cannot commit only part of a transfer.

## Concurrency and transaction boundary

Use a PostgreSQL transaction at `READ COMMITTED`. Lock both wallet rows in stable wallet-ID order with `SELECT ... FOR UPDATE`; this serializes transfers touching either wallet and reduces deadlock risk. Validate wallet existence and funds after taking the locks. Insert a `PENDING` transfer, then debit and credit balances, add both ledger entries, transition it to `PROCESSED`, and save the completed idempotency response before commit. An insufficient-funds result transitions the transfer to `FAILED` and stores the terminal response without writing ledger entries or changing balances. Any infrastructure error rolls back every change, including the idempotency claim.

Handlers validate and map HTTP input/output. The service owns the transfer workflow and transaction boundary. Repository code performs SQL operations. Domain types define request values, transfer state, and business errors.

## Testing and observability

Behavioral tests cover a successful transfer, balance and ledger consistency, replay of the same key, key reuse with different input, invalid/self transfers, missing wallets, insufficient funds, and transaction failure behavior. A PostgreSQL-backed concurrency test should issue competing debits against one funded wallet and confirm there is no overdraft and no partial ledger. The service logs request/transfer identifiers and failures without logging sensitive configuration; metrics are optional for this assignment.

## Assumptions and tradeoffs

- Amounts are integer minor units and only one currency is supported.
- Wallet creation, authentication, authorization, and transfer history are outside the initial scope.
- A PostgreSQL driver is the only non-standard-library runtime dependency; `net/http`, JSON handling, configuration, and orchestration use Go's standard library.
- The idempotency result is retained durably; no expiry policy is needed for this assignment.
- PostgreSQL is the target database, so SQL and concurrency behavior are designed for PostgreSQL rather than SQLite portability.

## Operational decisions to settle during implementation

- Select and pin the PostgreSQL driver version.
- Define database migration and local PostgreSQL startup instructions.
- Configure the repository CI variables for lint, format, and test commands, or update the workflow to use project-defined commands.
