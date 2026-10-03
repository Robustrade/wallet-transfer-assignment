# ADR-006: Lock both wallets in ID order with `FOR NO KEY UPDATE`

- **Status:** Accepted
- **Date:** 2026-10-03
- **Design doc:** [§7 Concurrency Control](../design.md#7-concurrency-control)

## Context

Two transfers can debit the same wallet at once, and the result must have
correct balances and no double spending (FR-5). Transfers in opposite
directions between the same two wallets must not deadlock. The key-claim
`INSERT` already takes `FOR KEY SHARE` locks on both wallets through its
foreign keys.

## Decision

At READ COMMITTED, lock both wallets with `SELECT … FOR NO KEY UPDATE`, one
statement each, in ascending ID order (sorted in Go), before reading the
source balance.

## Consequences

- The balance is read and changed under one lock, so there is no
  read-then-write race.
- A fixed lock order prevents deadlocks between transfers in opposite
  directions.
- `FOR NO KEY UPDATE` does not conflict with the foreign keys'
  `FOR KEY SHARE` locks; `FOR UPDATE` would, and two transfers on the same
  wallets would deadlock.
- Transfers sharing a wallet run one at a time; a busy wallet becomes a queue.
- There is no automatic retry: an unexpected abort returns `500`
  (requirements D-17).
