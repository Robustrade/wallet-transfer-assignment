# Wallet Transfer Service

A small HTTP service for wallet-to-wallet transfers with exactly-once request
handling, a double-entry ledger, and concurrency safety enforced by the
database.

The engineering rationale — schema, locking order, idempotency, failure modes
and tradeoffs — is in [`docs/design.md`](docs/design.md). This file covers how
to run and use it.

## Layout

```
cmd/walletd            entrypoint: config, migrations, HTTP server
internal/domain        entities, state machine, validation rules
internal/service       business logic, transaction boundaries, workflow
internal/repository    SQL; no business decisions
internal/httpapi       routing, request and response mapping, error codes
internal/database      connection, migrations, the few dialect differences
internal/dbtest        migrated database for tests, one per backend
```

## Running

With PostgreSQL, which is the intended target:

```sh
docker compose up --build
```

Or locally against SQLite, which needs nothing installed:

```sh
go run ./cmd/walletd
```

Configuration is two environment variables:

| Variable | Default | Notes |
| --- | --- | --- |
| `DATABASE_URL` | `sqlite://wallet.db` | `postgres://...` selects PostgreSQL |
| `PORT` | `8080` | |

The schema is applied at startup.

## API

Amounts are integers in minor units (cents). There is no floating point
anywhere in the money path.

```sh
curl -sX POST localhost:8080/wallets -d '{"id":"wallet_1","balance":1000}'
curl -sX POST localhost:8080/wallets -d '{"id":"wallet_2","balance":0}'

curl -isX POST localhost:8080/transfers -d '{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}'
```

```json
{
  "id": "tr_9f1c...",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "status": "PROCESSED",
  "createdAt": "2026-09-03T09:12:44.481Z",
  "updatedAt": "2026-09-03T09:12:44.492Z",
  "entries": [
    { "id": "le_3a...", "walletId": "wallet_1", "type": "DEBIT",  "amount": 100 },
    { "id": "le_7b...", "walletId": "wallet_2", "type": "CREDIT", "amount": 100 }
  ]
}
```

Sending that request again returns the identical body with an
`Idempotent-Replay: true` header, and moves no further money.

| Endpoint | Purpose |
| --- | --- |
| `POST /transfers` | Create a transfer, exactly once per `idempotencyKey` |
| `GET /transfers/{id}` | Read a transfer and its ledger entries |
| `POST /wallets` | Open a wallet with a starting balance |
| `GET /wallets/{id}` | Read a wallet balance |
| `GET /healthz` | Liveness |

Errors share one shape and a stable code:

```json
{ "error": { "code": "INSUFFICIENT_FUNDS", "message": "transfer could not be settled", "transferId": "tr_9f1c..." } }
```

| Code | Status |
| --- | --- |
| `INVALID_REQUEST` | 400 |
| `WALLET_NOT_FOUND`, `TRANSFER_NOT_FOUND` | 404 |
| `WALLET_ALREADY_EXISTS`, `IDEMPOTENCY_KEY_CONFLICT`, `CONCURRENT_REQUEST` | 409 |
| `INSUFFICIENT_FUNDS` | 422 |
| `INTERNAL_ERROR` | 500 |

## Testing

```sh
make test          # SQLite only; needs no services. This is what CI runs.
make lint
make fmt-check
```

To also exercise PostgreSQL, which is where the row-level locking is real:

```sh
docker compose up -d postgres
make test-postgres
```

Every test runs once per available backend. PostgreSQL is skipped, with a log
line, when `TEST_DATABASE_URL` is unset.

The suite covers successful transfers, insufficient funds, unknown wallets,
invalid amounts, idempotent replay, replay across a service restart, keys
reused with a different payload, transfers interrupted between commits, and the
schema constraints themselves. The concurrency tests reproduce the races: many
callers sharing one idempotency key, concurrent debits that together exceed a
balance, and opposing transfers that would deadlock under a naive locking
order.

## Original assignment

[`ASSIGNMENT.md`](ASSIGNMENT.md) and [`evaluation_guide.md`](evaluation_guide.md)
are the brief this repository answers.
