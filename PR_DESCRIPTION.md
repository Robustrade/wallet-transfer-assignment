# Wallet Transfer Service

## Summary

Implements `POST /transfers` with durable idempotency, stored wallet balances, a double-entry ledger, safe transfer state transitions, and concurrency-safe debits.

## Database schema

Four tables are used:

- `wallets`: wallet ID and non-negative stored balance.
- `transfers`: source/destination, amount, `PENDING|PROCESSED|FAILED`, failure reason, timestamps.
- `ledger_entries`: immutable `DEBIT`/`CREDIT` postings. `(transfer_id, entry_type)` is unique so a transfer cannot get duplicate debit or credit postings.
- `idempotency_records`: full idempotency key as primary key, canonical request SHA-256 hash, and unique reference to the original transfer.

DB constraints enforce positive amounts, non-negative wallet balances, valid statuses/types, different source/destination wallets, FKs, and ledger uniqueness. Indexes support wallet/ledger history lookups.

## Idempotency strategy

At the start of the transaction, the service obtains a PostgreSQL transaction-scoped advisory lock derived from the idempotency key. This solves the race where two first-time requests for the same never-before-seen key arrive simultaneously.

After acquiring that lock:

1. Read `idempotency_records` by the full key.
2. If it exists and its request hash matches, return the original transfer without touching wallets or ledger.
3. If it exists but the hash differs, return HTTP 409.
4. Otherwise continue and persist the key -> transfer mapping in the same DB transaction as the transfer.

The request hash includes source wallet, destination wallet, and normalized amount; the idempotency key itself is intentionally excluded from the hash.

An insufficient-funds result is persisted as `FAILED`, so repeating the same key returns the same failed transfer. Unexpected DB/runtime failures roll back the transaction, including the idempotency row, making a retry safe.

## Concurrency handling

Wallets are read with pessimistic write locks (`SELECT ... FOR UPDATE`). Both wallet IDs are always locked in lexical order.

Example with a source balance of 1000 and two concurrent transfers of 1000:

- request A locks the source, sees 1000, debits it, writes both ledger rows, and commits;
- request B waits for the source lock, then reads the committed balance of 0 and becomes `FAILED`;
- source balance never goes negative and only one pair of ledger rows is created.

Stable wallet lock ordering also reduces deadlock risk for simultaneous `A -> B` and `B -> A` transfers.

## Transaction boundary

A single `@Transactional` service method covers:

- idempotency locking/check
- PENDING transfer creation
- wallet row locks
- source debit
- destination credit
- DEBIT ledger row
- CREDIT ledger row
- terminal transfer state
- idempotency mapping

An unexpected failure rolls the entire operation back, preventing partial execution such as “debit committed but credit/ledger missing.”

## Transfer state machine

Allowed transitions are explicitly guarded in the entity:

- `PENDING -> PROCESSED`
- `PENDING -> FAILED`

A terminal transfer cannot transition again.

## Testing

Tests cover:

- successful balance movement
- exactly two balanced ledger rows
- same-key replay with no repeated side effect
- same key with a changed request -> conflict
- insufficient funds and failed-result replay
- two concurrent full-balance debits
- many simultaneous requests with the same idempotency key
- request fingerprint normalization
- transfer state transition guards

PostgreSQL integration tests use Testcontainers so row/advisory locking is exercised against PostgreSQL rather than an in-memory database.

## Assumptions / trade-offs

- Stored balances were chosen instead of deriving every balance from the ledger because reads are cheap while transaction atomicity keeps balances and ledger consistent.
- Pessimistic locking prioritizes correctness and simplicity over maximum throughput for a hot wallet.
- PostgreSQL advisory locks are database-specific, which is acceptable here because PostgreSQL is the preferred database in the assignment.
- Only `PROCESSED` transfers create ledger postings. `FAILED` attempts move no value and therefore create no accounting entries.
- The API contract has no currency field, so all wallet amounts are assumed to use the same currency/unit.

## AI usage

See `AI_USAGE.md` for the tool, workflow, and prompts used.
