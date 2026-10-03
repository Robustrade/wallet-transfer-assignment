# ADR-002: Store balances on the wallet row

- **Status:** Accepted
- **Date:** 2026-10-03
- **Design doc:** [§4 Data Model](../design.md#4-data-model)

## Context

FR-2 allows balances to be either derived from the ledger or stored and
updated during transfers. The balance check must be safe under concurrent
debits, and a balance must never go below zero.

## Decision

Store each balance in `wallets.balance` (`BIGINT`, `CHECK (balance >= 0)`),
updated in the same transaction that writes the ledger entries (ADR-005).

## Consequences

- The balance check reads one locked row, however large the ledger grows.
- The database itself refuses an overdraft, even if the code is wrong.
- The wallet row doubles as the lock that queues debits (ADR-006).
- Balance and ledger can drift apart if a code path updates one without the
  other. Tests guard against this with the reconciliation check
  `balance = opening_balance + credits − debits` (ADR-003, ADR-007).
