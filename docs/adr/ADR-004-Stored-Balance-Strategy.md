# ADR-004: Stored Balance Strategy

## Context

The service must support fast wallet balance reads while also preserving a ledger that can be used for reconciliation and auditing. Relying solely on ledger aggregation for every balance request would be inefficient for common queries.

## Decision

Maintain a stored balance on the wallet record while also appending ledger entries for every transfer. Add reconciliation support to compare the stored balance against the ledger-derived balance.

## Alternatives Considered

- Compute balance on demand from ledger entries only.
- Store only the ledger and use a separate projection cache for wallets.
- Use a cached balance with eventual reconciliation and no stored authoritative balance.

## Consequences

- Benefits:
  - Provides fast reads for wallet balances.
  - Preserves an authoritative ledger for audits and reconciliation.
  - Enables detection of drift between stored state and ledger state.
- Tradeoffs:
  - Introduces a second source of truth that must be kept consistent.
  - Requires reconciliation logic and tests to ensure correctness.
  - Any bug in balance updates can lead to balance drift that must be detected.

## Implementation Status

This ADR is implemented: the service updates `wallets.balance` within the same transaction that writes `ledger_entries` and `transfers`, minimizing risk of drift. The repository exposes a `reconcile` endpoint that computes ledger-derived balances and compares them to stored balances; integration tests cover reconciliation cases. Reviewers should note the explicit reconciliation path and ledger-trigger that enforces debit/credit equality.
