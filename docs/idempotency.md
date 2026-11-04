# Idempotency

The HTTP API calls `TransferService.ExecuteIdempotent(ctx, key, request)`. The key is required and limited to 255 characters.

## Request identity

The service hashes a canonical JSON representation of the source wallet ID, destination wallet ID, and integer amount using SHA-256. It does not hash raw HTTP JSON, so whitespace and JSON property ordering do not affect request identity. Reusing a key with a different normalized payload returns `409 Conflict`.

The raw idempotency key is stored as the database primary key because it is needed for lookup. It must not be written to application logs. Logs should use a one-way hash of the key when correlation is needed.

## Transaction and concurrency

The idempotency claim and transfer operation share one database transaction:

1. Insert the key and request hash using `INSERT ... ON CONFLICT DO NOTHING RETURNING`.
2. If inserted, this transaction owns the key and performs the transfer.
3. Persist the transfer ID, response status, and serialized transfer result before commit.
4. If the key already exists, read the existing record. A concurrent insert on the same PostgreSQL unique key waits for the owning transaction to commit or roll back.
5. A matching hash replays the stored transfer result; a different hash returns an idempotency conflict.

Because the claim and final response are committed with the transfer, a crash or rollback cannot leave a committed in-progress key. The database check constraint requires response status and response body to be either both null or both populated. The service completes both before committing.

## Replay semantics

Successful transfers are stored with status `201`. Deterministic business failures (insufficient funds, currency mismatch, or destination balance overflow) are stored with status `422` and replay the same `FAILED` transfer result and domain error. The HTTP handler maps those results to the same response envelope and status on replay.

Request validation failures occur before a transaction and are not stored. Missing-wallet and infrastructure failures roll back the transaction, including the idempotency claim, so a later retry with the same key can attempt the operation again. A conflicting payload never replaces the original idempotency record.

Idempotency records are retained indefinitely in this assignment implementation; no expiry/cleanup policy is configured.
