# ADR-003: Double Entry Ledger

## Context

The service must provide a reliable audit trail and support reconciliation of account balances. Transfers are financial in nature and require traceability for both source and destination movements.

## Decision

Implement a double-entry ledger model: each transfer creates exactly two ledger entries, one debit from the source wallet and one credit to the destination wallet.

### Implementation Details
- **Database Integrity**: A `CONSTRAINT TRIGGER` (`trg_enforce_ledger_balance`) is implemented at the database level. It is defined as `DEFERRABLE INITIALLY DEFERRED`, ensuring that the total debits equal total credits for any given `transfer_id` at the moment of transaction commit.
- **Strict Cardinality**: A unique constraint on `(transfer_id, entry_type)` enforces the requirement of exactly one debit and one credit per transfer.

## Alternatives Considered

- Update wallet balances directly without keeping separate ledger entries.
- Use a single ledger entry per transfer and infer the opposite side implicitly.
- Adopt full event sourcing for all balance changes.

## Consequences

- Benefits:
  - Enables a clear audit trail of every movement.
  - Makes reconciliation possible by re-computing balances from ledger entries.
  - Provides stronger accounting correctness and traceability.
  - **Defense in Depth**: The database trigger provides a final layer of safety even if application-layer validation is bypassed or contains bugs.
- Tradeoffs:
  - Stores more records than a balance-only approach.
  - Introduces additional write overhead for every transfer.
  - Requires careful validation to keep debit/credit pairs consistent.
  - **Processing Overhead**: The deferrable trigger adds a verification query to the end of every transfer transaction, increasing database CPU usage under high load.
