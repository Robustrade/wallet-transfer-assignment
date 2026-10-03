# ADR-008: Test through the API, without mocks

- **Status:** Accepted
- **Date:** 2026-10-03
- **Design doc:** [§11 Testing Strategy](../design.md#11-testing-strategy)

## Context

Tests must check behavior, not implementation details (NFR-4), and cover
transfer execution, idempotency, ledger correctness, failures, and
concurrency. The guarantees that matter most come from Postgres: locks,
constraints, and transactions.

## Decision

Use two levels. Table-driven unit tests cover the domain rules. Everything
else is tested through HTTP, against the real handler, service, and
repository on embedded Postgres (ADR-001), in `internal/apitest`. Each test
uses its own wallets and keys, so tests run in parallel.

## Consequences

- Tests assert the spec contract (status codes, bodies, database state), so
  refactoring internals does not break them.
- Concurrency and idempotency tests exercise real locks and constraints,
  which a fake could not.
- A failing API test points at a behavior rather than a layer, so locating
  the fault takes more digging.
- Every API test needs Postgres, so the suite is slower than pure unit tests.
