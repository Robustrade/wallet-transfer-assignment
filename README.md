# Wallet Transfer Service

Go service for wallet-to-wallet transfers with idempotency, double-entry ledger, concurrency safety, and structured logging. Supports **SQLite** (default, zero dependencies) and **PostgreSQL** (via `docker compose`).

## Quick start (SQLite)

```bash
go run ./cmd/server
```

The server listens on `http://localhost:8080` and seeds wallets `wallet_1`, `wallet_2`, and `wallet_3`.

## API

### Create transfer

```bash
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

Retry with the same `idempotencyKey` to verify idempotent replay (same response, no double debit).

### Health check

```bash
curl http://localhost:8080/health
```

## PostgreSQL (optional)

```bash
docker compose up -d
DB_DRIVER=postgres DATABASE_URL="postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable" go run ./cmd/server
```

## Run tests

```bash
go test ./... -v
```

With race detector and coverage (requires CGO and a C compiler, e.g. gcc):

```bash
CGO_ENABLED=1 go test ./... -race -cover
```

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_DRIVER` | `sqlite` | `sqlite` or `postgres` |
| `DATABASE_URL` | `wallet_transfer.db` | SQLite file path or PostgreSQL URL |
| `PORT` | `8080` | HTTP port |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `text` | `text` or `json` |

## Project layout

```
cmd/server/          — entry point, wiring, migrations, seed data
internal/config/     — environment configuration
internal/domain/     — models and domain errors
internal/handler/    — HTTP layer
internal/service/    — transfer workflow and idempotency
internal/repository/ — SQLite and PostgreSQL persistence
internal/database/   — DB factory and connection setup
migrations/          — SQL schema scripts (sqlite/, postgres/)
internal/middleware/ — request ID, logging, panic recovery
```

## Submission

1. Fork this repository and implement on branch `solution/<your-name>`.
2. Open a PR into `main` using [`.github/pull_request_template.md`](./.github/pull_request_template.md).
3. See [`ASSIGNMENT.md`](./ASSIGNMENT.md) for requirements and [`evaluation_guide.md`](./evaluation_guide.md) for the reviewer rubric.
