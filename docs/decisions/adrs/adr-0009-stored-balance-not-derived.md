---
kind: adr
status: accepted
owner: Kartik Mittal
last_reviewed: 2026-08-24
---

# ADR-0009 — Stored wallet balance, not derived from the ledger at read time

- **Status:** Accepted
- **Deciders:** Kartik Mittal (candidate)

## Context

`ASSIGNMENT.md` explicitly frames this as an open choice: "You may choose either: deriving
balance from the ledger, or maintaining a stored balance updated during transfers." ADR-0004
already implements a stored `balance` column on `wallets` — found on review to have been asserted
without the considered-options treatment the spec explicitly invites for this exact decision.

## Decision drivers

- Must guarantee correct balances under concurrent requests either way — this is graded directly.
- Should avoid recomputing an aggregate over an unbounded, ever-growing ledger on every balance
  check, which only gets slower as history accumulates.
- The chosen locking strategy (ADR-0003) already locks the wallet row directly for every
  transfer — the decision should build on that, not introduce a second mechanism alongside it.

## Considered options

1. **Derive balance from `SUM(ledger_entries)` at read/check time.** Rejected: cost grows with
   ledger size, and the concurrency story is worse, not better — a naive derive-then-check has
   exactly the read-then-write race ADR-0003 exists to close, and avoiding that race would mean
   locking the relevant ledger rows anyway, which is less natural to reason about than locking the
   wallet row directly and gains nothing over just storing the balance there.
2. **Stored `balance` column on `wallets`, updated transactionally alongside the ledger write,
   locked directly via `SELECT ... FOR UPDATE` (ADR-0003).** *Chosen.*

## Decision

`wallets.balance` (ADR-0004) is the authoritative, transactionally-updated value. `ledger_entries`
is the durable, append-only record of how it got there — not the source balance is computed from
at request time. A reconciliation invariant — `wallet.balance == initial_balance + sum(CREDIT) -
sum(DEBIT)` for that wallet — is a required test (ADR-0006's LT4), which is what actually keeps
the stored value and the ledger honest with each other, rather than merely trusting they can't
diverge.

## Consequences

**Good:** balance reads are O(1); the row already locked for correctness (ADR-0003) is the same
row that holds the answer, so no second mechanism is needed for balance integrity.

**Bad / risks:** the stored balance and the ledger are two representations of the same fact that
could in principle drift if some future code path updated one without the other — mitigated
entirely by ADR-0002/ADR-0003's rule that both are written in the same transaction, and by LT4's
reconciliation test catching any violation of that rule.
