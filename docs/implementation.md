# Implementation and API flow

## High-level flow

The application starts in `cmd/wallet-transfer/main.go` and does the following:

1. Builds a structured JSON logger.
2. Reads environment variables for database path, bind address, and port.
3. Opens the SQLite repository with the configured file path.
4. Creates the HTTP handler with the wallet service.
5. Starts the HTTP server and listens for shutdown signals.

## Request handling

The HTTP layer in `internal/wallet/http.go` registers the following routes:

- `GET /health`
- `POST /wallets`
- `GET /wallets/{walletID}`
- `POST /transfers`

The handler validates the `Content-Type` header, ensuring the request is JSON. It rejects empty or malformed payloads before they reach the service layer.

## Business logic

The `Service` struct in `internal/wallet/service.go` contains the core rules:

- wallet ID normalization and validation
- amount validation via `ParseMoney`
- transfer canonicalization using SHA-256 of a stable request representation
- transfer state and failure handling
- idempotency checks and replay detection

### Wallet creation

`CreateWallet` performs these steps:

1. Validates or generates a wallet ID.
2. Parses `initialBalance` if present.
3. Builds a wallet representation with `createdAt` in RFC3339Nano.
4. Executes repository logic inside an immediate transaction.
5. Inserts the wallet row and opening credit ledger record when the opening balance is greater than zero.

### Wallet retrieval

`GetWallet` normalizes the route parameter, then queries the repository for the wallet row.

### Transfer creation

`CreateTransfer` is the most important path:

1. Validates `idempotencyKey`, wallet IDs, and amount.
2. Rejects transfers where `fromWalletId == toWalletId`.
3. Canonicalizes the request into a stable hash.
4. Checks whether the idempotency key already exists.
5. If the same key exists with the same payload, it returns the original result.
6. If the same key exists with a different payload, it raises `ErrIdempotencyReuse`.
7. Reads both source and destination wallets.
8. Inserts a pending transfer row.
9. Determines whether the transfer is processed or failed.
10. Updates balances and inserts ledger entries when the transfer succeeds.
11. Marks the transfer as `PROCESSED` or `FAILED` and reloads the final row.

## Failure and status handling

The transfer status is mapped to a result object:

- `PROCESSED` — transfer succeeded
- `FAILED` — transfer was rejected without state mutation

Failure reasons include:

- `INSUFFICIENT_FUNDS`
- `BALANCE_LIMIT_EXCEEDED`

The HTTP layer converts these into `201` and `422` responses accordingly, while other domain errors become the appropriate HTTP status code.

## Transactionality

The repository wraps every wallet creation and transfer in an `BEGIN IMMEDIATE` transaction. This provides:

- consistent wallet balance updates
- atomic creation of transfer rows and ledger entries
- replay safety for idempotent requests
- automatic rollback if validation fails during the transaction

## Money representation

`Money` is a signed 64-bit integer representing minor units. Example:

- `1.00` -> `100`
- `12.5` -> `1250`
- `0.01` -> `1`

This format avoids floating-point drift while supporting precise business-accounting behavior.

## Important implementation choices

- `modernc.org/sqlite` is used for portable SQLite support
- the repository sets one open connection and one idle connection for serialized writes
- SQLite WAL mode is enabled for durable journal performance
- the HTTP layer intentionally rejects unknown JSON fields with `DisallowUnknownFields`
