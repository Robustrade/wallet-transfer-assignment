# Idempotency

The idempotent service entry point is `TransferService.ExecuteIdempotent(ctx, key, request)`. The key is required and limited to 255 characters.

## Request identity

The service hashes a canonical JSON object containing the source wallet ID, destination wallet ID, and integer amount. It does not hash raw HTTP JSON, so whitespace and JSON property ordering do not affect request identity. Reusing a key with a different normalized payload returns `service.ErrIdempotencyConflict`.

## Transaction and concurrency

The idempotency claim and transfer operation share one database transaction:

1. Insert the key and SHA-256 request hash using `INSERT ... ON CONFLICT DO NOTHING RETURNING`.
2. If inserted, this transaction owns the key and performs the transfer.
3. Persist the transfer ID, response status, and response body before commit.
4. If the key already exists, read the committed record in a subsequent statement. A concurrent insert on the same PostgreSQL unique key waits for the owning transaction to commit or roll back.
5. A matching hash replays the stored transfer result; a different hash returns an idempotency conflict.

Because the claim and response are committed with the transfer, a crash or rollback cannot leave a committed in-progress key. The table's response completeness constraint also prevents a partially populated response from being committed.

## Replay semantics

Successful transfers are stored with response status 201. Deterministic business failures are stored with status 422 and replay the same FAILED transfer and domain error. The HTTP layer will later map these service results to its response format. Infrastructure errors are not stored: the entire transaction rolls back, allowing a safe retry with the same key.

The service uses the existing PostgreSQL `idempotency_records` table. The repository requires a transaction implementation that supports `QueryRowContext` and `ExecContext`.
