# Wallet Transfer Service

Go implementation of the wallet transfer assignment. The service supports
idempotent wallet-to-wallet transfers, stored balances, and a double-entry ledger.

## Requirements

- Go 1.24
- Docker and Docker Compose
- `psql` (optional)
- `golangci-lint` for local linting

## Local Setup

Start PostgreSQL:

```bash
docker compose up -d postgres
```

Apply schema and seed wallets:

```bash
make migrate-up
```

Run the service:

```bash
make run
```

By default the service listens on `http://localhost:8080` and connects to:

```text
postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable
```

Override with:

```bash
PORT=9090 DATABASE_URL="postgres://..." make run
```

## Seed Wallets

The local seed migration creates two active USD wallets:

```text
00000000-0000-0000-0000-000000000001 balance 10000
00000000-0000-0000-0000-000000000002 balance 5000
```

## API Examples

Health:

```bash
curl http://localhost:8080/healthz
```

Create a transfer:

```bash
curl -i -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "abc123",
    "fromWalletId": "00000000-0000-0000-0000-000000000001",
    "toWalletId": "00000000-0000-0000-0000-000000000002",
    "amount": 100
  }'
```

Replay the same request with the same `idempotencyKey` to receive the cached original
response with `200 OK`.

Read a wallet:

```bash
curl http://localhost:8080/wallets/00000000-0000-0000-0000-000000000001
```

Read wallet ledger entries:

```bash
curl "http://localhost:8080/wallets/00000000-0000-0000-0000-000000000001/ledger?limit=50"
```

Read a transfer:

```bash
curl http://localhost:8080/transfers/<transfer-id>
```

## Development Commands

```bash
make test
make test-integration
make fmt
make fmt-check
make lint
make check-invariants
make migrate-down
```

Integration tests require a running PostgreSQL database and `TEST_DATABASE_URL`:

```bash
TEST_DATABASE_URL="postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable" make test-integration
```

`make check-invariants` verifies that processed transfers have balanced ledger entries
and stored wallet balances match opening balances plus processed ledger movement.

## CI

The existing GitHub Actions workflow runs lint, format check, and tests using
repository variables:

```text
LINT_CMD=golangci-lint run ./...
FORMAT_CHECK_CMD=test -z "$(gofmt -l .)"
TEST_CMD=go test ./...
CGO_ENABLED=0
```

## Design Notes

- Amounts are integer minor units. No floating point arithmetic is used.
- Missing `currency` defaults to `USD` for assignment payload compatibility.
- Service/domain code validates business rules and state transitions.
- PostgreSQL transactions, row locks, and constraints enforce correctness under
  concurrent requests.
- Wallets store `opening_balance` so invariant checks can compare current balance
  against opening balance plus ledger movement.
- Insufficient funds is persisted as a terminal `FAILED` transfer. Invalid request
  payloads and missing wallets are rejected before a transfer row is created.
