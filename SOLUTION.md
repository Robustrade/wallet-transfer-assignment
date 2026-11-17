# Wallet Transfer Service — Solution notes

Go HTTP API + PostgreSQL 16 implementing idempotent wallet-to-wallet transfers with a double-entry ledger.

Full design rationale (schema, idempotency, concurrency, failure modes, test plan): [`docs/design.md`](./docs/design.md).

AI usage and session prompts: [`docs/ai-disclosure.md`](./docs/ai-disclosure.md).

## Layout

| Package | Role |
| --- | --- |
| `handler` | HTTP decode/validation, status mapping |
| `service` | Transfer workflow, idempotency, locks orchestration, rules |
| `repository` | SQL (pgx), migrations, transactions |
| `model` | Entities, state transitions, canonical request hash |
| `dto` | API JSON shapes |
| `helpers` | Money / id normalization |
| `config` | Env / `.env` loading |
| `cmd/server` | Process entrypoint |

## How to run

```bash
# Postgres on host port 5433 (avoids clash with local 5432)
docker compose up -d

cp .env.example .env   # edit if needed
go run ./cmd/server
```

Defaults: listen `:8080`, DB `postgres://wallet:wallet@localhost:5433/wallet?sslmode=disable`.

### Smoke checks

```bash
curl -s http://localhost:8080/health

curl -s -X POST http://localhost:8080/wallets \
  -H 'Content-Type: application/json' \
  -d '{"id":"wallet_1","balance":1000}'

curl -s -X POST http://localhost:8080/wallets \
  -H 'Content-Type: application/json' \
  -d '{"id":"wallet_2","balance":0}'

curl -s -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"demo-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
```

## How to test

```bash
docker compose up -d

# Unit + integration + concurrency (needs wallet_test DB from compose init)
go test ./...

# Race detector on the money path
go test ./service/ -race

# Skip DB-backed tests when Postgres is unavailable
SKIP_DB_TESTS=1 go test ./...
```

`TEST_DATABASE_URL` defaults to `postgres://wallet:wallet@localhost:5433/wallet_test?sslmode=disable` (separate from `DATABASE_URL` so test `TRUNCATE` does not wipe manual data).

Lint / format (as used locally):

```bash
golangci-lint run ./...
gofmt -l .
```

## Endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/health` | Liveness |
| `POST` | `/wallets` | Create wallet |
| `GET` | `/wallets/{id}` | Get wallet |
| `POST` | `/transfers` | Create transfer (idempotent) |
| `GET` | `/transfers/{id}` | Get transfer |
