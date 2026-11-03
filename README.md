# Wallet Transfer API

This project exposes a small HTTP service for creating wallets and transferring funds between them using SQLite as the durable backing store.

## Project goals

- Create wallets with an optional opening balance
- Fetch wallet balances by ID
- Transfer funds between wallets with idempotency protection
- Record immutable ledger entries for each transfer
- Reject invalid requests and failed transfers with explicit status codes

## Service at a glance

The service is split into three layers:

- HTTP layer: request validation and JSON responses in `internal/wallet/http.go`
- Service layer: business rules and transfer orchestration in `internal/wallet/service.go`
- Repository layer: SQLite persistence and atomic transaction handling in `internal/wallet/repository.go`

## Running the service

From the repository root:

```bash
go run ./cmd/wallet-transfer
```

The application reads these environment variables:

- `HOST` — bind address, default `127.0.0.1`
- `PORT` — HTTP port, default `8000`
- `WALLET_DB_PATH` — SQLite file path, default `wallets.sqlite3`
- `LOG_LEVEL` — `DEBUG`, `INFO`, `WARN`, or `ERROR`

Example:

```bash
HOST=0.0.0.0 PORT=8080 WALLET_DB_PATH=/tmp/wallets.sqlite3 LOG_LEVEL=INFO go run ./cmd/wallet-transfer
```

## API documentation

See [docs/api.md](./docs/api.md) for endpoint contracts, request examples, and response codes.

The OpenAPI/Swagger specification is available in [docs/swagger.yaml](./docs/swagger.yaml).

## Implementation and logic notes

See [docs/implementation.md](./docs/implementation.md) for the request flow, transaction rules, and business logic.

## Database documentation

See [docs/database.md](./docs/database.md) for the SQLite schema, triggers, invariants, and data model.

## Example curl calls

Create a wallet:

```bash
curl -X POST http://127.0.0.1:8000/wallets \
  -H 'Content-Type: application/json' \
  -d '{"walletId":"source","initialBalance":100}'
```

View a wallet:

```bash
curl http://127.0.0.1:8000/wallets/source
```

Transfer money:

```bash
curl -X POST http://127.0.0.1:8000/transfers \
  -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"transfer-1","fromWalletId":"source","toWalletId":"destination","amount":25.5}'
```

## Notes on money handling

The API accepts and returns decimal amounts, while the service stores money as
integer minor units (cents). For example:

- `12.5` becomes `1250`
- `1.230` is normalized to `123`
- amounts must be positive and support at most two decimal places

The response serializes `1250` minor units as `12.5`. This keeps money
arithmetic exact in SQLite and avoids floating-point precision issues.
