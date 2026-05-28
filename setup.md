# Setup / Run / Test Guide

This repo contains a small wallet transfer service written in Go with Postgres as the persistence layer.

## Prerequisites

- Go `1.24+`
- Docker + Docker Compose
- (Optional) `psql` if you want to run migrations without Docker

## Run with Docker (recommended)

From the repo root:

```bash
make up
```

This starts:

- `postgres` on `localhost:5432`
- `migrate` (runs schema migrations from `db/migrations/`)
- `api` on `http://localhost:8080`

All services join a docker network named `kuku_assignment`.

Stop:

```bash
make down
```

## Run manually (without Docker Compose)

### 1) Start Postgres

Use any local Postgres (or run one via Docker):

```bash
docker run --rm -p 5432:5432 \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=wallet \
  postgres:16
```

### 2) Apply migrations

Option A (Docker migrate image):

```bash
docker run --rm \
  -v "$PWD/db/migrations:/migrations:ro" \
  migrate/migrate:v4.17.1 \
  -path /migrations \
  -database 'postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable' \
  up
```

Option B (`psql`):

```bash
psql 'postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable' \
  -f db/migrations/001_init.up.sql
```

### 3) Run the API

```bash
export DATABASE_URL='postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable'
export PORT=8080
go run ./cmd/api
```

## Manual Testing via curl

### Health check

```bash
curl -s http://localhost:8080/healthz
```

### Create wallets

Create a wallet with an initial balance:

```bash
curl -s -X POST http://localhost:8080/wallets \
  -H 'content-type: application/json' \
  -d '{"initialBalance":1000}'
```

Create another wallet:

```bash
curl -s -X POST http://localhost:8080/wallets \
  -H 'content-type: application/json' \
  -d '{"initialBalance":0}'
```

Copy both `walletId` values for the next steps.

### Get wallet balance

```bash
curl -s http://localhost:8080/wallets/<walletId>
```

### Create transfer (success)

```bash
curl -s -X POST http://localhost:8080/transfers \
  -H 'content-type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"<fromWalletId>","toWalletId":"<toWalletId>","amount":100}'
```

### Idempotency replay (same response, no duplicate side effects)

Send the exact same request again:

```bash
curl -s -X POST http://localhost:8080/transfers \
  -H 'content-type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"<fromWalletId>","toWalletId":"<toWalletId>","amount":100}'
```

You should see the exact same JSON response as the first call.

### Idempotency mismatch (409)

Reuse the same idempotency key with a different payload:

```bash
curl -i -s -X POST http://localhost:8080/transfers \
  -H 'content-type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"<fromWalletId>","toWalletId":"<toWalletId>","amount":101}'
```

Expected: `409 Conflict`.

### Insufficient funds (409)

```bash
curl -i -s -X POST http://localhost:8080/transfers \
  -H 'content-type: application/json' \
  -d '{"idempotencyKey":"insufficient-1","fromWalletId":"<fromWalletId>","toWalletId":"<toWalletId>","amount":9999999}'
```

Expected: `409 Conflict`, and balances should remain unchanged.

## Running tests

### Unit/compile tests

```bash
make test
```

### Integration tests (require Postgres)

The integration tests will be skipped unless one of these env vars is set:

- `TEST_DATABASE_URL` (preferred)
- `DATABASE_URL`

Example (using the docker-compose Postgres):

```bash
export TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable'
go test ./... -run TestTransfer_
```

