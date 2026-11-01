# Wallet transfer design

The service moves a positive integer amount from one wallet to another. One database transaction writes the transfer, both ledger rows, both balances, and the stored HTTP result. A later call with the same idempotency key returns that result and does not move money again.

## API

`POST /transfers` creates a transfer. The body carries `idempotencyKey`, `fromWalletId`, `toWalletId`, and `amount`. The amount is a positive integer in minor units. The key is required. A lost response cannot be retried safely without one.

| Status | Result |
| --- | --- |
| 201 | The transfer is `PROCESSED`. |
| 422 | The transfer is `FAILED`. Balances do not change. |
| 409 | The key already belongs to a different payload. |
| 404 | A wallet does not exist. The key is not stored. |
| 400 | The body is invalid. The service does not open a transaction. |

A replay returns the stored status and the stored body. The response sets `Idempotent-Replayed` to `true` only on a replay.

`POST /wallets` creates a wallet with `id` and `initialBalance`. `GET /wallets/{id}` returns the stored balance. `GET /transfers/{id}` returns one transfer and its ledger entries. These routes let a caller fund a wallet and read the result. The assignment requires `POST /transfers`.

## Data

`wallets.balance` and `wallets.initial_balance` are non-negative `BIGINT` values. `initial_balance` does not change after insert. The stored balance stays equal to `initial_balance + credits - debits`.

`transfers.idempotency_key` is unique. The two wallet ids differ. `amount` is positive. Status is `PENDING`, `PROCESSED`, or `FAILED`. A failed row stores `failure_reason`. The other statuses leave that column null.

`ledger_entries` stores one `DEBIT` and one `CREDIT` for a processed transfer. Both rows share the transfer id and the amount. `UNIQUE (transfer_id, entry_type)` blocks a second debit or a second credit. A failed transfer has no ledger rows.

`idempotency_records` stores the key, a payload hash, the transfer id, the HTTP status, and the response body. The body column is `TEXT`. A replay can then return the original bytes. `JSONB` would reorder keys and change the body.

## Transaction

The isolation level is `READ COMMITTED`.

The service inserts the idempotency key with `ON CONFLICT DO NOTHING`. PostgreSQL waits when another open transaction already inserted that key. If this insert wins, the request owns the key. If it loses, the service locks the existing row and reads the stored result. A different hash returns 409. If the owner rolled back, the row is gone and this request inserts the key.

The service then locks the two wallet rows. It locks the lower id first, then the higher id, in two statements. One `SELECT ... ORDER BY id FOR UPDATE` can take locks before it sorts. Opposing transfers can deadlock on that plan. Two statements in sorted id order take the locks in one order every time.

The service inserts the transfer as `PENDING`. If the source balance is too small, it sets the status to `FAILED` and commits HTTP 422. If the credit would overflow `int64`, it does the same with reason `balance_overflow`. Neither case writes ledger rows or changes balances.

Otherwise the service inserts the debit and the credit. It subtracts the amount only where `balance >= amount`. It adds the amount to the destination, sets the status to `PROCESSED`, stores HTTP 201, and commits.

Callers never observe `PENDING`. The status change is part of the same transaction. If the process stops before commit, the database rolls every write back, including the key. The client retries that key.

Each request context expires after 8 seconds. That is shorter than the 10 second write timeout. A transfer still waiting on a wallet lock is canceled, and the open transaction rolls back. The handler returns 504 when it can still write the response.

On process stop, the server waits 5 seconds for handlers to finish. If one is still running, it closes the connection. That cancels the request context before the database pool closes.

`CHECK (balance >= 0)` rejects an overdraft when a writer skips the lock.

## Idempotency

The hash is SHA-256 of the source id, the destination id, and the decimal amount. JSON field order does not change the hash.

A validation failure returns 400 before the service claims the key. A missing wallet rolls the claim back. The client can create the wallet and retry the same key. Insufficient funds and overflow stay stored, so the same payload keeps the same business result.

## Logs

A completed call logs the transfer id, the idempotency key, both wallet ids, the HTTP status, and the replay flag. Each value is a structured field. The service does not write the raw body to the log.

## Tests

Domain tests cover invalid amounts, equal wallet ids, and illegal status changes.

Service tests use an in-memory store. They cover a processed transfer, a failed transfer with an empty ledger, a replay, a hash mismatch, overflow, and a missing wallet that does not consume the key.

PostgreSQL tests run when `DATABASE_URL` is set. They open a separate database named `{database}_test`, then cover the same outcomes on that database, plus parallel debits, parallel copies of one key, and opposing transfers. Truncation stays in the test database. When `DATABASE_URL` is unset, those tests skip. `go test ./...` still passes.

## Assumptions

Amounts belong to one currency. The service does not convert them.

Idempotency keys do not expire.

The service does not authenticate callers.
