# ADR-005: Run each transfer in one database transaction

- **Status:** Accepted
- **Date:** 2026-10-03
- **Design doc:** [§5 Transfer Flow](../design.md#5-transfer-flow)

## Context

A transfer touches the idempotency key, the transfer row, two balances, and
two ledger entries. Requests may be duplicated, retried, or cut off midway,
and a transfer must execute atomically (FR-1, NFR-2). Transfers move through
`PENDING`, `PROCESSED`, and `FAILED` (FR-4).

## Decision

Process each transfer synchronously in one database transaction: claim the
key by inserting the transfer as `PENDING`, lock both wallets, then either
mark it `FAILED`, or move the balances, write both ledger entries, and mark
it `PROCESSED`; then commit (requirements D-9, D-11). `PENDING` exists only
inside the transaction.

## Consequences

- Atomicity comes from Postgres: an error or crash before `COMMIT` rolls
  everything back, so there are no partial transfers to repair. If `COMMIT`
  itself fails, the outcome is unknown but still all or nothing; a retry
  with the same key replays or runs as new (design §9).
- No `PENDING` transfer is ever committed, so no recovery job is needed.
- The response always carries a final state, `PROCESSED` or `FAILED`.
- `PENDING` is internal bookkeeping; clients never observe it.
- Request latency includes lock waits; each request has a 5 s deadline
  (design §9).
