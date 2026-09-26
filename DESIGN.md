# Wallet Transfer Service Design

## API Contract

- `GET /` returns a JSON endpoint index.
- `POST /wallets` creates a wallet with a non-negative opening balance. Duplicate wallet IDs return `409 Conflict`.
- `GET /wallets/{walletId}` returns its current balance.
- `GET /wallets/{walletId}/transfers` returns transfers involving that wallet, newest first, with a stable transfer-ID tie-breaker.
- `POST /transfers` accepts `idempotencyKey`, `fromWalletId`, `toWalletId`, and a positive integer `amount` in minor units.
- A new successful transfer returns `201 Created`; a replay returns the stored status and response body. Insufficient funds returns a stored `422 Unprocessable Entity` result.
- Unknown wallets return `404`; invalid requests return `400`; a key reused with a different canonical request returns `409 idempotency_key_conflict`; an active claim that remains incomplete after configured retries returns `409 idempotency_in_progress` with `Retry-After`.

Transfers to the same wallet are rejected. Unexpected persistence failures return a generic `500`; internal errors are logged.

## Persistence and Ledger

PostgreSQL is the production persistence backend. Versioned SQL migrations create `wallets`, `transfers`, `ledger_entries`, and `idempotency_records`, with foreign keys, primary/unique keys, positive amount and valid-state checks, and indexes for wallet transfer history and ledger lookups. Migration startup is serialized with a PostgreSQL advisory transaction lock.

Wallet balances are stored on `wallets`. Creating a wallet with a positive opening balance atomically records a debit to `system:funding` and an equal credit to the wallet. Each successful transfer inserts a `PENDING` transfer, updates both wallet balances, writes equal debit and credit ledger entries, transitions to `PROCESSED`, and stores the replayable response in one transaction. Insufficient funds records a terminal `FAILED` transfer and response without balance changes or transfer ledger entries. Any unexpected error rolls back the transfer transaction.

Ledger entries use an operation ID so both opening entries and transfer entries have exactly one debit and one credit posting. Opening postings have no transfer ID; transfer postings reference their transfer.

## Idempotency

The request is canonically JSON-encoded and SHA-256 hashed, avoiding ambiguous delimiter serialization. A durable idempotency claim is acquired using `INSERT ... ON CONFLICT DO NOTHING RETURNING`. Each in-progress claim has a random owner token and a 30-second lease. On key conflict, the stored hash is checked: a mismatch returns conflict; a completed matching request replays the saved status/body; an unexpired claim is polled up to `MAX_CLAIM_ATTEMPTS`, then returns `409 idempotency_in_progress`.

An expired claim can be taken over under a row lock. The owner token is checked and held under lock throughout transfer processing, fencing off a delayed former owner. Completion and release are conditional on the current owner token. Thus a process crash before transfer processing does not strand a key permanently, while concurrent requests still cannot apply duplicate side effects. Wallet and request validation occurs before claim creation. Insufficient-funds results are terminal and consume the key.

## Concurrency

Transfers run at `READ COMMITTED`. The service sorts the two wallet IDs and locks both wallet rows in that stable order with `SELECT ... FOR UPDATE`. This prevents overspending and avoids circular waits for opposite-direction transfers. Balance changes, transfer state, ledger rows, and idempotency completion commit atomically. Destination additions are checked for signed 64-bit overflow before SQL update.

## Configuration, TLS, and Testing

`DATABASE_URL` is required. `PORT` defaults to `8080`; `MAX_CLAIM_ATTEMPTS` defaults to `3`; `SSL_ENABLED` defaults to false. When HTTPS is enabled, both `TLS_CERT_FILE` and `TLS_KEY_FILE` may be supplied; if neither is supplied, the application generates a development self-signed localhost certificate.

Fast unit and HTTP tests use repository mocks and need no database. Tagged PostgreSQL integration tests use Testcontainers by default, or `TEST_DATABASE_URL` when an existing PostgreSQL instance is supplied. They cover transfer atomicity, idempotency/recovery, opening-ledger postings, migrations, and opposite-direction locking.
