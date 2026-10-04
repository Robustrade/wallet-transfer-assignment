# ADR 0003: Idempotency keys

## Status

Accepted

## Context

The API must provide exactly-once transfer semantics when the client sends `idempotencyKey`. Duplicate delivery can be concurrent, or it can happen after the process restarts. The key has to survive in PostgreSQL, not in process memory.

## Decision

`idempotency_records` stores the key, a SHA-256 fingerprint of `fromWalletId`, `toWalletId`, and `amount`, and the transfer id. `transfers.idempotency_key` is also unique, so a second transfer row cannot be inserted for the same key even if the application forgets the first table.

The fingerprint is `sha256(from + "|" + to + "|" + decimalAmount)`.

Workflow:

1. `INSERT` the key. `ON CONFLICT DO NOTHING` waits if another transaction inserted the same key and has not committed.
2. If this transaction inserted the row, it owns the attempt and performs the transfer, then sets `transfer_id` before commit.
3. If it did not insert the row, it loads the original transfer. Matching fingerprint returns that transfer. A different fingerprint returns a conflict and does not write.
4. A deferred trigger forbids committing an idempotency row without `transfer_id`.

Validation failures (bad JSON, same wallet, unknown wallet, overflow) roll the transaction back, so the key is not consumed. Insufficient funds commits a `FAILED` transfer. Repeating that key returns the same failed transfer. A later attempt after the wallet is topped up needs a new key. That matches the usual rule that the key identifies one outcome, not "try until success".

The HTTP body is rendered from the transfer row. Status code comes from the status: `201` for `PROCESSED`, `422` for `FAILED`. Replay builds the same view, so the stored response is the row, not a cached HTTP payload.

## Consequences

- Idempotency is durable across restarts because it is committed with the transfer.
- Concurrent duplicates block on the unique index and then read the winner's row.
- The key is global. The assignment has no authenticated caller or tenant.

## Alternatives

A cache with a TTL loses the result on expiry and on restart, so a retry can move money twice. An idempotency table without a unique transfer key leaves a hole if a bug writes a second transfer. Both unique constraints close that hole. Caching the encoded JSON body would also work and would couple the table to HTTP field names. Re-rendering from the transfer row keeps one source of truth.
