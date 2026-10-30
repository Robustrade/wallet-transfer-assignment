# Wallet Transfer Service

A Java 17 / Spring Boot REST API for wallet-to-wallet transfers. PostgreSQL stores wallet balances, transfer states, idempotency records, and an append-only double-entry ledger. Flyway creates the schema and local sample wallets.

## Requirements

- JDK 17 or newer (the project targets Java 17)
- Maven 3.8+
- Docker Desktop with Docker Compose

## Start PostgreSQL

From the project root:

```powershell
docker compose up -d postgres
docker compose ps
```

Wait until PostgreSQL reports `healthy`. The local database is `wallet_transfer`, username `wallet`, password `wallet`, port `5432`. These credentials are for local development only.

## Run the application

In a second terminal, from the project root:

```powershell
mvn spring-boot:run
```

Flyway applies `src/main/resources/db/migration/V1__init.sql` at startup. The migration seeds `wallet_1` (1000.00), `wallet_2` (500.00), and `wallet_3` (0.00). The API listens on `http://localhost:8080`.

To build and test:

```powershell
mvn clean verify
```

The integration and concurrency tests use the configured PostgreSQL database. Start the database before running the test suite. Tests create uniquely named wallets; use a disposable local database for testing.

## API

### Create a transfer

`POST /transfers`

```json
{
  "idempotencyKey": "demo-transfer-001",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100.00
}
```

Example first response (`201 Created`):

```json
{
  "transferId": "<generated-uuid>",
  "status": "PROCESSED",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100.00,
  "failureReason": null,
  "replayed": false
}
```

Send the same request again with the same key to replay the original result (`200 OK`). Reusing the key with a different payload returns `409 Conflict`. Insufficient balance is stored as a `FAILED` transfer and replaying that key returns the same failure result. Invalid request fields return `400`; an unknown wallet returns `404`.

### Read a wallet balance

`GET /wallets/{id}`

```json
{
  "walletId": "wallet_1",
  "balance": 900.0000
}
```

### Read a transfer

`GET /transfers/{transferId}`

### Read a wallet ledger

`GET /wallets/{id}/ledger`

### Health

`GET /actuator/health`

## Error response shape

```json
{
  "error": "IDEMPOTENCY_KEY_CONFLICT",
  "message": "The idempotency key has already been used with a different request"
}
```

The exact message includes the relevant key or validation detail. Do not expose the development database credentials outside local use.

## Correctness decisions

- **Atomicity:** balance changes, transfer state, ledger entries, and idempotency record are written in a single PostgreSQL transaction. Unexpected persistence failures roll back the transaction.
- **Concurrency:** wallet rows are locked with pessimistic write locks. The two wallet IDs are locked in lexicographic order so opposite-direction transfers acquire locks consistently. PostgreSQL serializes competing changes to the same wallet.
- **Idempotency:** the request payload is hashed and persisted alongside the idempotency key. Replays with the same payload return the original transfer; a different payload for an existing key is rejected. Unique constraints are the database backstop for races.
- **Ledger:** a processed transfer writes one debit and one credit of equal positive amounts. Both entries are committed with the balance changes. Failed transfers do not create ledger entries.
- **Money:** `BigDecimal` and PostgreSQL `NUMERIC(19,4)` avoid binary floating-point arithmetic.
- **State machine:** transfers start `PENDING` and transition to `PROCESSED` or `FAILED` in the transaction.

## Scope and assumptions

This is an assignment implementation, not a production payment platform. It does not include authentication, currency conversion, external payment rails, or distributed messaging. A `PENDING` state is used during the transaction, but it is not intended to represent an asynchronous workflow. The local migration seeds sample wallets for easy manual testing.

## GitHub submission checklist

1. Create a branch from `main`, for example `solution/abhishek-mane`.
2. Run `mvn clean verify` with PostgreSQL available and inspect the results.
3. Review `ASSIGNMENT.md`, `DESIGN.md`, and `evaluation_guide.md` before finalizing the PR.
4. Explain schema design, transaction boundaries, idempotency, concurrency control, test results, and trade-offs in the PR description.
5. Complete `AI_USAGE.md` honestly and attach the requested prompt/session transcript according to the assignment's AI-use policy.
6. Open a pull request into `main`; do not claim checks passed unless the CI results confirm it.
