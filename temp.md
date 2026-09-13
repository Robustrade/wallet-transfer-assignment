## 16. README

Create `README.md`:

```markdown
# Wallet Transfer Service

Go service for transactional wallet-to-wallet transfers.

## Requirements

- Go 1.22+
- Docker
- Docker Compose

## Start Dependencies

```bash
docker compose up -d
```

## Apply Database Schema

```bash
docker compose exec -T postgres psql \
  -U wallet \
  -d wallet \
  < migrations/001_init.sql
```

## Install Dependencies

```bash
go mod tidy
```

## Format and Test

```bash
gofmt -w ./cmd ./internal
go test ./...
```

Integration tests are skipped unless `TEST_DATABASE_URL` is configured.

Run integration tests:

```bash
TEST_DATABASE_URL=postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable \
go test ./internal/repository -v
```

## Run API

```bash
export DATABASE_URL='postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable'
export REDIS_URL='redis://localhost:6379/0'
export HTTP_ADDR=':8080'

go run ./cmd/api
```

## Create Wallets

```bash
curl -i -X POST http://localhost:8080/wallets \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "wallet_1",
    "currency": "USD",
    "initialBalance": 1000
  }'
```

```bash
curl -i -X POST http://localhost:8080/wallets \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "wallet_2",
    "currency": "USD",
    "initialBalance": 0
  }'
```

Balances use integer minor units. For example, `1000` can represent `$10.00`.

## Create Transfer

```bash
curl -i -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{
    "idempotencyKey": "transfer-request-1",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 100
  }'
```

Expected successful response:

```json
{
  "transferId": "UUID",
  "status": "PROCESSED",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

The response status is `201 Created`.

## Retry the Same Transfer

```bash
curl -i -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{
    "idempotencyKey": "transfer-request-1",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 100
  }'
```

The same transfer ID and response are returned. The response contains:

```text
X-Idempotent-Replay: true
```

## Reuse the Key With Different Data

```bash
curl -i -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{
    "idempotencyKey": "transfer-request-1",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 999
  }'
```

Expected response:

```text
409 Conflict
```

## Read Wallet

```bash
curl -i http://localhost:8080/wallets/wallet_1
```

## Read Transfer

```bash
curl -i http://localhost:8080/transfers/TRANSFER_UUID
```

## Health Checks

```bash
curl -i http://localhost:8080/healthz
curl -i http://localhost:8080/readyz
```

## Useful SQL Checks

Check wallet balances:

```bash
docker compose exec postgres psql \
  -U wallet \
  -d wallet \
  -c 'SELECT id, currency, balance FROM wallets ORDER BY id;'
```

Check transfers:

```bash
docker compose exec postgres psql \
  -U wallet \
  -d wallet \
  -c 'SELECT id, idempotency_key, status, amount FROM transfers ORDER BY created_at;'
```

Check ledger entries:

```bash
docker compose exec postgres psql \
  -U wallet \
  -d wallet \
  -c 'SELECT id, wallet_id, transfer_id, entry_type, amount FROM ledger_entries ORDER BY id;'
```

## Stop Everything

```bash
docker compose down
```

Remove containers and database volumes:

```bash
docker compose down -v
```
```

## 17. Run Everything

From the repository root:

```bash
go mod tidy
docker compose up -d
docker compose exec -T postgres psql -U wallet -d wallet < migrations/001_init.sql
gofmt -w ./cmd ./internal
go test ./...
go run ./cmd/api
```

In a second terminal:

```bash
curl -X POST http://localhost:8080/wallets \
  -H 'Content-Type: application/json' \
  -d '{"id":"wallet_1","currency":"USD","initialBalance":1000}'

curl -X POST http://localhost:8080/wallets \
  -H 'Content-Type: application/json' \
  -d '{"id":"wallet_2","currency":"USD","initialBalance":0}'

curl -i -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{
    "idempotencyKey":"abc123",
    "fromWalletId":"wallet_1",
    "toWalletId":"wallet_2",
    "amount":100
  }'
```

Expected resulting balances:

```text
wallet_1 = 900
wallet_2 = 100
```

The important correctness properties are implemented in PostgreSQL:

- unique idempotency key
- request fingerprint validation
- row-level wallet locking
- deterministic lock ordering
- atomic balance updates
- exactly two balanced ledger entries
- safe transfer state transitions
- retry-safe stored response results