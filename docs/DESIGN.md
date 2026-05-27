# Wallet Transfer Service — Design Notes

## Problem

`POST /transfers` must move funds between wallets with exactly-once semantics when `idempotencyKey` is supplied, double-entry ledger rows, and safe behavior under concurrent debits.

## Schema

| Table | Purpose |
|-------|---------|
| `wallets` | Stored balance per wallet (`balance >= 0`) |
| `transfers` | Transfer lifecycle (`PENDING` → `PROCESSED` / `FAILED`), unique `idempotency_key` |
| `ledger_entries` | Exactly one `DEBIT` and one `CREDIT` per processed transfer (`UNIQUE (transfer_id, type)`) |
| `idempotency_records` | Durable idempotency key → request hash → transfer id |

## Idempotency

1. Compute `request_hash = SHA-256(fromWalletId|toWalletId|amount)`.
2. Insert into `idempotency_records` (primary key on `idempotency_key`); on conflict, load existing row.
3. If hash differs → `409 Conflict`.
4. If `transfer_id` present → return that transfer (no side effects).
5. Otherwise create `PENDING` transfer, link idempotency row, process once.

Retries after commit but before the client receives the response replay safely because the idempotency row and transfer already exist.

## Concurrency

- **Pessimistic row locks** (`SELECT … FOR UPDATE`) on both wallets.
- Wallets are locked in **sorted id order** to avoid deadlocks.
- Balance check and updates happen inside a single `@Transactional` boundary.
- `ledger_entries` uniqueness prevents duplicate postings on retry.

## Failure modes

| Case | Behavior |
|------|----------|
| Insufficient funds | `FAILED`, no ledger rows, balances unchanged |
| Unknown wallet | `FAILED` with reason |
| Same key, different payload | `409 Conflict` |

## Layers

- **web** — HTTP mapping, validation, error responses
- **service** — transfer workflow, idempotency, locking
- **repository** — Spring Data JPA persistence
- **domain** — entities and state enums

## Testing

Integration tests (H2 in PostgreSQL compatibility mode) cover happy path, idempotency, conflict, insufficient funds, and concurrent debits.
