# Wallet Transfer Service

Wallet-to-wallet transfer API with idempotency, transactional consistency, and basic observability.

## Implementation summary

This project includes all major implementation items needed for assignment and production-style local development:

- Makefile with build/run/test/lint/migration/docker targets.
- Dockerfile and `docker-compose.yml` for app + PostgreSQL setup.
- `.env` and `.env.example` for environment-based configuration.
- DTO and domain model separation (`internal/model/dto` and `internal/model`).
- GORM-compatible DB model tags for persistence entities.
- Structured request logging with `trace_id`, `idempotency_key`, and `transfer_id`.
- Idempotency metrics (`transfer_idempotency_hits_total`).
- Server lifecycle handling in `internal/server` with graceful shutdown.
- Standardized API error response shape.
- Test suite expanded for core business and API flows.

## What this service guarantees

- **Atomic transfer**: debit and credit happen in one DB transaction.
- **No double processing**: idempotency key + request hash prevents duplicate execution.
- **Concurrency safety**: wallets are locked in deterministic order.
- **Auditability**: transfer + ledger entries are persisted.

## Quick start (for a new developer)

### 1) Prerequisites

- Go `1.26+`
- Docker and Docker Compose
- `psql` client (optional, useful for manual checks)

### 2) Setup environment

```bash
cp .env.example .env
```

### 3) Start PostgreSQL and run migrations

```bash
docker compose up -d postgres
psql -h localhost -U postgres -d wallet -f migrations/schema.sql
psql -h localhost -U postgres -d wallet -f migrations/seed.sql
```

> Default credentials are already in `.env.example`.

### 4) Build and run the API

```bash
make build
./bin/wallet-service
```

Successful build output is expected to show `Building wallet-service...` and then return to the shell prompt.

Service endpoints:
- API: `http://localhost:8080/api/v1/transfers`
- Health: `http://localhost:8080/api/v1/health`
- Metrics: `http://localhost:8080/metrics`

Backward-compatible endpoints:
- `http://localhost:8080/transfers`
- `http://localhost:8080/health`

### Alternative: run everything with Docker

```bash
make docker-up
```

## API contract

### `POST /api/v1/transfers`

Request:

```json
{
  "idempotencyKey": "txn-1",
  "fromWalletId": 1,
  "toWalletId": 2,
  "amount": 100
}
```

Success response:

```json
{
  "transferId": 1,
  "status": "PROCESSED"
}
```

Error response:

```json
{
  "status": "failure",
  "error": {
    "message": "insufficient balance",
    "code": "TRANSFER_FAILED"
  }
}
```

## Idempotency behavior

- First request with a key: executes transfer and stores response.
- Same key + same payload: returns stored response.
- Same key + different payload: rejected.
- Same key while in progress: returns conflict (`409`).

## Project structure

```text
Makefile                       # build/test/lint/docker/dev targets
Dockerfile                     # multi-stage image build
docker-compose.yml             # postgres + app services
cmd/server/                    # application entrypoint
internal/handler/              # HTTP handlers (DTO <-> service mapping)
internal/service/              # transfer business logic
internal/repository/           # repository interfaces
internal/repository/postgres/  # postgres/gorm implementation
internal/model/                # domain + persistence models (DB entities)
internal/model/dto/            # transport models for API request/response
internal/db/                   # DB initialization
internal/logger/               # zap logger + request context
internal/metrics/              # prometheus counters
migrations/                    # schema and seed SQL
test/                          # integration-style tests with mocks
```

## `model` vs `dto` (is this structure correct?)

Yes, this split is correct and recommended:

- `internal/model`: internal domain/persistence structures used by repository/service (`Wallet`, `Transfer`, `LedgerEntry`, etc.).
- `internal/model/dto`: HTTP API payload contracts used by handlers (`TransferRequestDTO`, `TransferResponseDTO`, `ErrorResponseDTO`).

This separation keeps DB concerns and API contract concerns independent.

## Run tests and coverage

```bash
go test ./...
go test ./... -coverpkg=./internal/... -coverprofile=coverage.out
go tool cover -func=coverage.out
make coverage-core
```

Current core coverage target command:

- `make coverage-core` -> validates core package coverage and currently reports `91.8%`.

## Requirement checklist

- [x] Makefile with docker-compose and developer commands
- [x] DTO and model folder structure
- [x] DB model tags for persistence entities
- [x] Structured logs with request context fields
- [x] Idempotency hit metric
- [x] Migration support via Make targets
- [x] Coverage command and tests
- [x] Standardized error response format
- [x] Environment templates (`.env.example`)
- [x] Server setup with graceful shutdown

## Notes

- Default server config is defined in `internal/server`.
- Logs are structured JSON via Zap.
- Prometheus counters are exposed at `/metrics`.
