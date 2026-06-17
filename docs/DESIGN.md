# Wallet Transfer Service — Design Notes

## Problem

Support wallet-to-wallet transfers with API-level idempotency, double-entry ledger recording, correct balances under concurrency, and safe transfer state transitions.

## API Contract

### POST /transfers

Creates a transfer. When `idempotencyKey` is supplied, duplicate requests return the original result without duplicate side effects.

### GET /wallets/{walletId}

Returns the current stored balance for a wallet.

### GET /transfers/{transferId}

Returns transfer details and linked ledger entries.

Amounts are stored as integer values (smallest currency unit) to avoid floating-point errors.

## Database Schema

| Table | Purpose |
|---|---|
| `wallets` | Stored balance with non-negative constraint |
| `transfers` | Transfer lifecycle and unique `idempotency_key` |
| `ledger_entries` | Double-entry audit rows (DEBIT + CREDIT per processed transfer) |
| `idempotency_records` | Cached HTTP response for replay |

Key constraints:

- `transfers.idempotency_key` is unique
- `ledger_entries` has `UNIQUE (transfer_id, entry_type)` ensuring exactly one debit and one credit
- `wallets.balance >= 0` enforced at database level
- `from_wallet_id <> to_wallet_id` enforced at database level

## Idempotency Strategy

1. Compute a SHA-256 hash of `(idempotencyKey, fromWalletId, toWalletId, amount)`.
2. If an `idempotency_records` row exists:
   - matching hash → deserialize and return cached response + HTTP status
   - mismatched hash → `409 Conflict`
3. For new keys:
   - insert `transfers` row as `PENDING` (unique key prevents duplicate creation)
   - insert placeholder `idempotency_records` row
   - execute transfer logic inside the same transaction
   - persist serialized response before commit

Duplicate requests after commit replay the stored response. Retries are safe across process restarts because state is durable in PostgreSQL.

## Concurrency Strategy

All transfer processing runs in a single `@Transactional` boundary.

- Source and destination wallets are locked with `SELECT ... FOR UPDATE`
- Balance check, debit, credit, ledger inserts, and status update happen atomically
- Concurrent debits on the same wallet serialize on the row lock

This prevents read-then-write races and double spending.

## Failure Modes

| Scenario | Behavior |
|---|---|
| Insufficient funds | Transfer marked `FAILED`, no ledger entries, balances unchanged |
| Wallet not found | `404`, transaction rolls back |
| Same idempotency key, different body | `409 Conflict` |
| Duplicate replay | Original HTTP status and body returned |

## Testing Strategy

- Integration tests with Testcontainers PostgreSQL for end-to-end behavior
- Idempotency replay and conflict tests
- Insufficient funds with replay verification
- Concurrency test with parallel debits from the same wallet
- Unit test for transfer state transition rules

## Observability

Structured logging at INFO for the `com.wallet` package. Metrics and distributed tracing are out of scope for this assignment.
