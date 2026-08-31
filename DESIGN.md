# Wallet transfer service design

## Contract

`POST /transfers` accepts an idempotency key, source wallet ID, destination wallet ID,
and a positive integer amount in the wallet's smallest currency unit. It returns the
transfer's ID, status, and a `replayed` flag. Reusing a key with a different request is
rejected; reusing it with an identical request returns the original completed result.

## Data model

- `wallets`: current balance in integer minor units; a `CHECK (balance >= 0)` constraint
  prevents persisted negative balances.
- `transfers`: one state-machine record per requested transfer (`PENDING`, `PROCESSED`,
  or `FAILED`).
- `ledger_entries`: immutable debit and credit rows. A unique `(transfer_id, entry_type)`
  constraint permits one debit and one credit only.
- `idempotency_records`: a unique key and a canonical request fingerprint. It protects
  duplicate delivery and lets the service return the original response.

## Transaction and concurrency strategy

The service uses SQLite in WAL mode and executes the complete workflow in one write
transaction (`BEGIN IMMEDIATE`). This serializes competing writers, so two debits of the
same wallet cannot both observe the same available balance. The transaction atomically
creates the idempotency and transfer records, checks and updates balances, inserts both
ledger entries, and marks the transfer `PROCESSED`. Any error rolls back every change.

SQLite is appropriate for this small, self-contained assignment. In production on
PostgreSQL, the same workflow would use a transaction plus deterministic `SELECT ... FOR
UPDATE` locks on both wallet rows, a unique idempotency-key index, and retry handling for
serialization/deadlock errors.

## Failures and retries

Insufficient funds creates no transfer or ledger side effect; the transaction rolls back.
When a request is retried after a successful commit, its stored fingerprint and transfer
are returned without another debit. A caller that reuses a key for different input
receives a conflict, preventing accidental key reuse from silently returning the wrong
operation.

## Observability and tests

The handler logs no sensitive request body. The service exposes typed validation,
not-found, insufficient-funds, and idempotency-conflict errors for transport mapping.
Tests cover successful transfer, replay, conflicting key reuse, insufficient funds,
ledger pairing, and concurrent debit attempts.
