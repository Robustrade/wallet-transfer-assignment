# ADR-004: Store the idempotency key on the transfer

- **Status:** Accepted
- **Date:** 2026-10-03
- **Design doc:** [§4 Data Model](../design.md#4-data-model),
  [§6 Idempotency](../design.md#6-idempotency)

## Context

Every request carries an idempotency key (requirements D-2). A repeated
request must return the original result without repeating side effects, even
after a crash or restart. The assignment suggests an `idempotency_records`
table, but only stored transfers use up a key (spec §5), so each key belongs
to exactly one transfer.

## Decision

Store the key in `transfers.idempotency_key` (`VARCHAR(255) NOT NULL
UNIQUE`). Claim it with `INSERT … ON CONFLICT (idempotency_key) DO NOTHING`
inside the transfer's transaction. A replay rebuilds the response from the
stored transfer's state.

## Consequences

- The key and every side effect commit together or not at all (ADR-005), so a
  duplicate can never repeat a side effect.
- The unique index, not application code, detects duplicates, and it makes a
  concurrent request with the same key wait for the first.
- One table fewer than the assignment suggests; the PR must explain this
  deviation.
- No response is stored. This works because the state alone decides the
  status and body; a richer response body would need storing.
- Keys are never deleted, so the column grows with every transfer.
