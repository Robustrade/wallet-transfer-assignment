# Wallet Transfer Service Design

## Amount Representation

Amounts are represented as signed 64-bit integers in minor currency units.

For example:

- `100` means 100 cents
- `1050` means 1050 cents

The service does not use floating point arithmetic.

## Database Source of Truth

PostgreSQL is the source of truth for:

- Wallet balances
- Transfer state
- Idempotency records
- Ledger entries

Redis is not required for correctness. Redis is used only as a short-lived cache for wallet reads.

## Idempotency

The `(transfers.idempotency_key, transfers.shard_id)` is the primary key.

The request also stores a SHA-256 request hash based on:

- `fromWalletId`
- `toWalletId`
- `amount`

The transfer transaction first attempts:

```sql
INSERT INTO transfers (...) VALUES (...)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING id;
```

Behavior:

1. If the insert succeeds, this request owns the transfer and processes it.
2. If the insert conflicts, the existing transfer is read.
3. If the stored request hash differs, the API returns HTTP 409.
4. If the hash matches, the stored original response is returned.
5. The original transfer ID and response body are reused.

The database unique constraint is the correctness mechanism. Redis is deliberately not used for idempotency because cache availability must not affect financial correctness.

## Sharding and asynchronous transaction boundary

`idempotency_key` is the transfer's routing key. The application computes a stable
`shard_id` from it and every transfer and outbox event carries that value. The
database uses `(idempotency_key, shard_id)` as the idempotency primary key; a
request is always routed to the same shard before it is read or written.

Creating a transfer is a short transaction:

1. Insert the transfer as `PENDING`.
2. Insert a unique `DEBIT_WALLET` outbox event for the transfer.
3. Commit.

The outbox event is the Kafka-equivalent handoff. A worker consumes it
idempotently, locks only the payer wallet, deducts the balance, and writes that
wallet's `DEBIT` ledger entry in one transaction. It then creates the unique
`CREDIT_WALLET` event. A second worker locks only the receiver wallet, credits
it, writes its `CREDIT` ledger entry, and marks the transfer `PROCESSED`.

Every event has a unique `(transfer_id, event_type)` key, so retries and
duplicate delivery cannot repeat a balance mutation. If credit cannot be
completed after debit, the debit worker writes a compensating `CREDIT` entry
and restores the payer balance before marking the transfer `FAILED`. If a
post-credit failure occurs, a compensating `DEBIT` entry reverses the receiver
credit. Compensation is itself idempotent by a unique transfer/entry type key.

The API returns `202 Accepted` for a newly queued transfer and returns the
stored response for an idempotent replay. It never performs wallet mutation in
the request transaction.

## Concurrency

Wallet rows are locked using:

```sql
SELECT ...
FROM wallets
WHERE id = $1
FOR UPDATE;
```

Wallet IDs are sorted before locking. This gives every transfer a consistent lock order and reduces deadlock risk when two transfers involve the same wallets in reverse directions.

Concurrent debits against one source wallet serialize on the source wallet row. The second transaction sees the balance after the first transaction commits and cannot double-spend.

## Ledger Integrity

Every processed transfer must have exactly:

- one debit entry for the source wallet
- one credit entry for the destination wallet
- equal debit and credit amounts
- amounts equal to the transfer amount

A deferred PostgreSQL constraint trigger enforces these rules at transaction commit.

The trigger also rejects ledger entries for pending or failed transfers.

## Transfer States

Allowed transitions:

```text
PENDING -> PROCESSED
PENDING -> FAILED
```

The database trigger rejects transitions from `PROCESSED` or `FAILED` to another state.

`PENDING` exists as an intermediate state inside the transfer transaction. The synchronous API does not intentionally commit a pending transfer.

## Failure Modes

### Insufficient funds

- Transfer is marked `FAILED`.
- No wallet balances change.
- A failed transfer response is stored.
- The debit event is acknowledged without publishing a credit event.
- Retry with the same idempotency key returns the same failure.

### Idempotency key reused with different payload

- No new transfer is created.
- HTTP 409 is returned.
- Existing transfer remains unchanged.

### Database failure

- PostgreSQL transaction rolls back.
- No partial balance or ledger changes remain.
- Client retry with the same idempotency key can safely retry.

### Network failure after commit

The client may not receive the response even though PostgreSQL committed the transfer. A retry with the same idempotency key reads the committed transfer and returns the original result.

## Observability

The application logs:

- server startup
- Redis availability
- request method
- request path
- request duration
- unexpected errors

The API exposes:

```text
GET /healthz
GET /readyz
```

Workers log event type, shard, transfer ID, and retry/compensation decisions.

## Tradeoffs

The implementation uses stored balances because transfer concurrency requires locking wallets anyway. Deriving balances from the ledger would make reads more expensive and would still require transactional ledger writes.

Redis is intentionally non-critical. It improves wallet GET latency but is invalidated after successful transfers.