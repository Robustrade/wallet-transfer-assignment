# Wallet Transfer Service: Approach

## Goal and API behavior

The service transfers an integer amount, in minor currency units, between two existing wallets. It exposes:

- `POST /transfers` with `idempotencyKey`, `fromWalletId`, `toWalletId`, and positive `amount`.
- `GET /wallets/{id}` to read a wallet's stored balance.

Successful transfers return `201` and a `PROCESSED` transfer. Insufficient funds creates a terminal `FAILED` transfer and returns `422`. Repeating a request with the same key and payload returns the persisted transfer body and original HTTP status. Reusing a key with a different payload returns `409`. Invalid requests return `400`; unknown wallets return `404`.

Wallet creation and authentication are outside this assignment's scope. Wallets are seeded directly in PostgreSQL. Amounts are integers to avoid floating-point currency arithmetic; the service does not assign a currency or perform exchange-rate conversion.

## Layers

- **HTTP handler** (`internal/httpapi`) binds JSON, maps domain errors to HTTP responses, and returns service results.
- **Service** (`internal/service`) validates transfer requests and exposes transfer and wallet operations.
- **PostgreSQL repository** (`internal/store`) owns SQL and the transaction that coordinates idempotency, wallet balances, transfer state, and ledger writes.
- **Domain models** (`internal/domain`) define request validation and response shapes.

This keeps transport concerns out of the transaction implementation and keeps persistence details out of handlers.

## Data integrity

PostgreSQL stores wallets with nonnegative integer balances, transfers with positive amounts and constrained states, idempotency records keyed uniquely by client key, and immutable ledger entries. A unique `(transfer_id, type)` constraint allows at most one debit and one credit per transfer. Foreign keys and checks protect references and basic invariants.

Deferred database triggers reject a committed `PENDING` transfer and require every committed `PROCESSED` transfer to have exactly two ledger entries with equal debit and credit totals. Ledger entries cannot be updated or deleted. The transaction writes one `DEBIT` entry for the source and one `CREDIT` entry for the destination. Failed transfers have no ledger entries because no value moved.

Balances are stored for efficient reads and updated in the same transaction as the ledger. The balance is therefore the operational read model; the ledger is the immutable record of successful value movement.

## Transaction and concurrency strategy

One PostgreSQL `READ COMMITTED` transaction contains the complete transfer workflow:

1. Insert an idempotency reservation using `ON CONFLICT DO NOTHING`.
2. Read and lock the key's record. A concurrent request using the same key waits for the unique-key conflict to resolve, then sees the committed result.
3. Compare the stored payload with the request. Return a conflict for a mismatch or replay the stored transfer for a match.
4. Lock both wallet rows in stable wallet-ID order with `SELECT ... FOR UPDATE`.
5. Create the transfer as `PENDING`, then either mark it `FAILED` for insufficient funds or debit/credit balances, insert both ledger sides, and mark it `PROCESSED`.
6. Store the transfer ID and terminal HTTP status in the idempotency record, then commit.

The locks prevent two transfers from spending the same available balance at once. Stable lock ordering also reduces deadlocks for simultaneous transfers in opposite directions. All dependent writes commit or roll back together. `PENDING` is an in-transaction state and cannot pass the deferred commit check.

## Idempotency and retry behavior

The idempotency record contains the original wallet IDs and amount, transfer ID, and response status. The primary key on the request key serializes duplicate requests in PostgreSQL. Matching retries return the original transfer and status without applying any additional balance or ledger changes. A key reused with a different payload returns `409` and does not change the original record.

Insufficient-funds responses are persisted as terminal failures. Retrying the same key after the source wallet is funded still returns the original failure; the caller must use a new key for a new attempt. A crash before commit leaves neither a transfer nor an idempotency record. A crash after commit leaves a replayable result.

## Failure handling and observability

Client errors use a stable JSON error object with a code and message. Unexpected persistence errors roll back and return a generic `500`; details go to the server logger rather than the response. Echo request logging, request IDs, and recovery middleware provide basic operational visibility.

The server accepts either a PostgreSQL connection URL or individual host, port, user, password, database, and `DB_SSL_MODE` settings. CLI flags override environment-derived defaults. Local env files are loaded only when explicitly selected with `ENV_FILE`; the integration test likewise loads a file only when `TEST_ENV_FILE` is set. Copy `test-local.env.example` to `test-local.env` for local use. The default SSL mode is `disable` for local development; deployments using remote PostgreSQL should choose an appropriate secure mode.

## Testing approach

`go test ./...` runs unit tests for database URL construction and local env-file behavior. PostgreSQL integration tests exercise the HTTP handler against an isolated temporary schema and cover:

- successful transfer, stored balances, and both balanced ledger entries;
- exact replay of a successful request and conflict for a reused key with a different payload;
- insufficient funds and replay of the terminal failure;
- concurrent debits that must not overspend.

Set `TEST_DATABASE_URL` or `DATABASE_URL`, or provide the individual `DB_*` values in the environment, to run the integration test. To load a local file, set `TEST_ENV_FILE=../../test-local.env` when running the package test. The test account needs permission to create and drop schemas. Without database settings, the integration test skips.

## Tradeoffs and next steps

This implementation assumes one PostgreSQL database is the transactional source of truth. Row locks serialize activity on each affected wallet; this is simple and correct for the assignment, though a high-volume deployment may need connection-pool tuning and workload monitoring. The startup schema application is convenient for a small exercise; a production deployment should use versioned migrations. Authentication, authorization, rate limiting, and a wallet provisioning API are also outside the assignment scope.
