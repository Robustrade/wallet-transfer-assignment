# ADR-005: Idempotency Design

## Context

The transfer API is exposed to clients that may retry requests due to network timeouts or uncertain responses. The system must avoid duplicate transfers and preserve exactly-once semantics at the API level.

## Decision

Implement idempotency using a persistent idempotency record store keyed by client-provided idempotency keys. Persist request metadata and outcome so repeated requests with the same key return the original result instead of creating duplicate transfers.

### Implementation Details
- **Payload Verification**: A `request_hash` (generated from the request payload) is stored to detect "idempotency key recycling." If a client retries a key with a different amount or wallet, the system rejects it as a conflict.
- **Response Replay**: The complete outcome, including the HTTP status code and JSON response body, is persisted. Subsequent retries return this cached response without re-triggering business logic.
- **Lifecycle Tracking**: Records use a state machine (`PENDING`, `COMPLETED`, `FAILED`). This allows the system to distinguish between an active request, a successful one, and a failed attempt that might be retryable.
- **Atomic Execution**: All side effects (ledger entries, wallet updates, and idempotency status updates) are executed within a single ACID transaction.

## Alternatives Considered

- Rely only on client-generated transfer IDs as the unique key.
- Use in-memory deduplication with a TTL.
- Use a message queue with deduplication at the queue layer.
- Accept eventual duplicate processing and rely on compensating actions.

## Consequences

- Benefits:
  - Provides stronger idempotent behavior across retries and restarts.
  - Supports replay-safe API semantics and predictable duplicate handling.
  - Avoids duplicate ledger entries and duplicate balance changes.
- Tradeoffs:
  - Requires persistent storage for idempotency records.
  - Introduces additional write overhead for transfer requests.
  - Requires careful handling of record uniqueness, expiry, and cleanup policy.
  - **Storage Growth**: Storing full JSON response bodies results in higher storage consumption over time compared to simple boolean flags.
  - **Stall Risks**: Requests interrupted during the `PENDING` phase may require a timeout or cleanup mechanism to prevent the key from being "locked" indefinitely.

## Implementation Status

The implementation follows this ADR with the following specifics:

- Pre-claim semantics: the service now inserts an `idempotency_records` row with `status = 'PENDING'` at the start of a transfer attempt (inside the transfer transaction). This claim serializes concurrent requests using the same idempotency key and minimizes wasted work.
- Payload verification: the server computes a canonical SHA256 `request_hash` from the request payload (server-side canonicalization of `idempotencyKey`, `fromWalletId`, `toWalletId`, `amount`) to avoid client-side mismatches.
- Response replay: on successful completion the implementation updates the idempotency record to `COMPLETED` and stores the canonical `response_json` and `status_code` for exact replay.
- Cleanup: a background worker transitions stale `PENDING` records to `FAILED` after a configurable TTL and exposes a Prometheus gauge `wallet_transfer_idempotency_pending` to monitor pending claim counts.

## Notes for reviewers

- The implementation diverges from a simpler create-on-success approach by pre-claiming idempotency keys. This choice favors lower-latency duplicate detection and clearer semantics under contention at the cost of an extra write for new keys. Tests cover concurrent retries and the cleanup behavior; reviewers should ensure the TTL and transition policy align with deployment expectations.
