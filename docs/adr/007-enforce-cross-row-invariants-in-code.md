# ADR-007: Enforce cross-row invariants in code, not triggers

- **Status:** Accepted
- **Date:** 2026-10-03
- **Design doc:** [§8 Consistency Invariants](../design.md#8-consistency-invariants)

## Context

Single-row rules are database constraints: a balance is never negative, a key
is unique, every ledger entry belongs to a transfer, and a transfer has at
most one DEBIT and one CREDIT. Rules that span rows cannot be plain
constraints: a `PROCESSED` transfer has exactly two matching entries, a
`FAILED` one has none, and no transfer is committed as `PENDING`.

## Decision

Enforce cross-row rules through the single transaction (ADR-005), and verify
them with reconciliation queries after every test, including the concurrent
ones (ADR-008). Use no deferred constraint triggers.

## Consequences

- The rules live in one readable code path instead of trigger logic.
- A code change that breaks them is caught by the tests, not by the database.
- Data written outside the service, such as by hand in `psql`, is not checked.
