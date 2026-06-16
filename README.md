# Wallet Transfer Service

Production-grade Go service for wallet-to-wallet transfers with idempotency, double-entry ledger, and PostgreSQL. HTTP layer uses **Gin** with thin handlers over a service/repository stack.

## Features

- `POST /transfers` — atomic transfers with idempotency keys
- `GET /wallets/{id}/balance` — wallet balance lookup
- `GET /health` — database health check
- Embedded migrations, Docker Compose, structured logging

## Prerequisites

- Go 1.25+
- Docker (for PostgreSQL and integration tests)
- Optional: [golangci-lint](https://golangci-lint.run/)

## Quick Start

```bash
# Start PostgreSQL
make up

# Run migrations (also run automatically on server start)
make migrate

# Start the API
make run
```

Or run everything in Docker:

```bash
docker compose up --build
```

## Example Request

```bash
curl -s -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{
    "idempotencyKey": "abc123",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 100
  }'
```

Seed wallets (`wallet_1` = 10000, `wallet_2` = 5000) are created by migration.

## Configuration

Copy `.env.example` to `.env` or set:

| Variable | Default |
|----------|---------|
| `DATABASE_URL` | `postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable` |
| `HTTP_PORT` | `8080` |
| `LOG_LEVEL` | `info` |

## Testing

```bash
# Unit tests
go test ./... -race -count=1

# Integration tests (requires Docker)
go test -tags=integration ./tests/integration/... -race -count=1

# All tests
make test
```

## Linting

```bash
make lint
make fmt-check
```

## CI Commands

Set these GitHub repository variables (or use defaults from Makefile):

```bash
LINT_CMD=golangci-lint run ./...
FORMAT_CHECK_CMD=test -z "$(gofmt -l .)"
TEST_CMD=go test ./... -tags=integration -race -count=1 -cover
```

## Architecture

```text
cmd/server          HTTP entrypoint (Gin engine + graceful shutdown)
internal/handler    Gin routes, JSON binding, response mapping
internal/service    Transfer workflow and idempotency
internal/repository PostgreSQL access
internal/domain     Entities and errors
internal/migrate    Embedded SQL migrations
tests/integration   Behavioral tests with testcontainers
```

See [docs/design.md](docs/design.md) for idempotency, concurrency, and failure-mode details.

## Assignment

This repository implements the [Wallet Transfer Assignment](ASSIGNMENT.md). Submit via pull request to `main` on branch `solution/<your-name>`.
