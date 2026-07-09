# Wallet Transfer Service Design

## Problem statement

The service exposes wallet-to-wallet transfers with API-level exactly-once behavior for a caller-supplied `idempotencyKey`. A successful transfer debits one wallet, credits another wallet, and records a balanced double-entry ledger. A failed business transfer is recorded as `FAILED` without partial balance or ledger side effects.

## API contract

### `POST /transfers`

Request:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Responses:

- `201 Created` for a newly processed transfer.
- `200 OK` for a replay of the same idempotency key and identical payload.
- `409 Conflict` if an existing idempotency key is reused with a different payload.
- `422 Unprocessable Entity` for a recorded business failure such as insufficient funds.
- `400 Bad Request` for validation errors.

The response includes transfer state, ledger entries for processed transfers, and a `replay` flag.

### Optional read APIs

- `GET /wallets/{id}` returns a wallet and its stored balance.
- `GET /transfers/{id}` returns the transfer and its ledger entries.
- `GET /healthz` returns service health.

## Persistence

SQLite is used for local portability. The schema has:

- `wallets`: stored balance with `CHECK (balance >= 0)`.
- `transfers`: immutable transfer request data, state, and failure reason.
- `ledger_entries`: double-entry rows with `DEBIT` and `CREDIT` types and a uniqueness constraint on `(transfer_id, type)`.
- `idempotency_records`: unique idempotency key, canonical request hash, and transfer id.

Amounts are represented as integer minor units. Floating point money is intentionally avoided.

## Idempotency behavior

The idempotency key is inserted in the same database transaction as the transfer. The service stores a SHA-256 hash of the canonical request payload excluding the idempotency key. On duplicate requests:

- Same key and same payload returns the original transfer result.
- Same key and different payload returns `409 Conflict`.
- Duplicate requests never create additional transfers or ledger entries.

## Transaction and concurrency strategy

Each transfer runs in a serializable database transaction. The SQLite store uses a single open database connection and a busy timeout, so writes are serialized and duplicate/concurrent requests wait for the active writer instead of observing partial state.

The debit operation uses a guarded update:

```sql
UPDATE wallets
SET balance = balance - ?
WHERE id = ? AND balance >= ?
```

This is the final no-overspend guard. If it affects no rows, the transfer is marked `FAILED` and no ledger entry is written.

## State transitions

Transfers start as `PENDING` and can only move to `PROCESSED` or `FAILED`. Repository updates enforce this with `WHERE state = 'PENDING'`, preventing retries from mutating terminal transfers.

## Failure modes

- Validation errors are rejected before opening the transaction.
- Missing wallets and insufficient funds become durable `FAILED` transfer records.
- Unexpected database errors roll back the whole transaction.
- Processed transfers always have exactly two ledger entries: one debit and one credit.

## Observability

The service logs startup configuration and exposes `/healthz`. In a larger production system I would add request IDs, structured logs, metrics for transfer outcomes, and latency histograms.

## Testing strategy

Tests use a real temporary SQLite database and verify:

- successful transfer execution,
- stored balances,
- double-entry ledger balancing,
- idempotent replay,
- idempotency key conflict,
- insufficient funds failure without side effects,
- concurrent transfers from the same wallet without overspending.
