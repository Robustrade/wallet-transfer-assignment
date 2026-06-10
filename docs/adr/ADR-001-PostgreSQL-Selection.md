# ADR-001: PostgreSQL Selection

## Context

The wallet transfer service requires a durable, transactional datastore capable of supporting account balances, ledger entries, and concurrent updates. The system must enforce strong consistency, support schema evolution, and provide reliable persistence for both the current wallet state and the transaction ledger.

## Decision

Use PostgreSQL as the primary datastore for wallets, transfers, ledger entries, and idempotency records.

## Alternatives Considered

- In-memory database: fast but not durable and not suitable for production stateful transfer data.
- NoSQL document stores: good for scale but weaker transactional guarantees and harder support for atomic balance updates across wallets.
- Distributed ledger / blockchain: overkill for this use case and would introduce unnecessary operational complexity.
- File-based persistence: lacks concurrent transactional safety and would be difficult to maintain at scale.

## Consequences

- Benefits:
  - Strong ACID transactions for balance updates and ledger entry creation.
  - Native support for unique constraints and relational integrity.
  - Good fit for query patterns that combine wallet state, transfers, and reconciliation.
- Tradeoffs:
  - Requires operational effort to run and maintain PostgreSQL.
  - Adds schema migration management and database dependency in deployment.
  - Potential performance bottleneck if not sized or tuned properly for concurrent load.
