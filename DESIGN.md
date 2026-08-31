# Wallet transfer service design

## Contract

`POST /transfers` accepts an idempotency key, source wallet ID, destination wallet ID,
and a positive integer amount in the wallet's smallest currency unit. It returns the
transfer's ID, status, and a `replayed` flag. Reusing a key with a different request is
rejected; reusing it with an identical request returns the original completed result.

## Data model

- `wallets`: UUID primary key and current balance in integer minor units; a `CHECK (balance >= 0)` constraint
  prevents persisted negative balances.
- `transfers`: UUID primary key and one state-machine record per requested transfer (`PENDING`, `PROCESSED`,
  or `FAILED`).
- `ledger_entries`: immutable debit and credit rows. A unique `(transfer_id, entry_type)`
  constraint permits one debit and one credit only.
- `idempotency_records`: a unique key and a canonical request fingerprint. It protects
  duplicate delivery and lets the service return the original response.

## Transaction and concurrency strategy

The service uses PostgreSQL. Each request is executed in a database transaction that
locks its two wallet rows using `SELECT ... FOR UPDATE`, always in ascending wallet-ID
order. Consequently, two transfers that debit the same wallet serialize safely, while
transfers involving unrelated wallets can continue in parallel. Locking in a deterministic
order prevents the common `A -> B` / `B -> A` deadlock pattern.

The transaction atomically creates the idempotency and transfer records, checks and
updates balances, inserts both ledger entries, and marks the transfer `PROCESSED`. Any
unexpected error rolls back every change. A unique idempotency-key constraint handles a
race between duplicate requests; the losing request reads and returns the committed
original result.

## Failures and retries

Insufficient funds creates no transfer or ledger side effect; the transaction rolls back.
When a request is retried after a successful commit, its stored fingerprint and transfer
are returned without another debit. A caller that reuses a key for different input
receives a conflict, preventing accidental key reuse from silently returning the wrong
operation.

## Operations and tests

The HTTP layer emits structured logs with a request ID, method, route, status, and
duration; it never logs request bodies or balance values. `GET /metrics` exposes a small
Prometheus-compatible metric set for request volume, processed/failed transfers, and
idempotent replays.

`GET /wallets/{id}/balance` returns the current stored balance. `GET
/wallets/{id}/transfers?limit=50` returns recent transfers where the wallet was either
the source or destination. Both endpoints use UUID wallet IDs and return typed
not-found/validation errors.

PostgreSQL serialization failures and deadlocks are retried up to three times with a
short bounded backoff. Each attempt preserves the same idempotency key; a committed
attempt is therefore returned safely on a retry rather than causing duplicate side
effects. Tests cover successful transfer, replay, conflicting key reuse, insufficient
funds, ledger pairing, concurrent debit attempts, balances, and transfer history.
`docker compose up -d` starts the local PostgreSQL instance used by the service and
integration tests.
