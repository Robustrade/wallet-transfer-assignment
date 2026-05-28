# Wallet Transfer Service (Go + Postgres)

Small HTTP service that supports wallet-to-wallet transfers with:

- idempotency (`idempotencyKey` → exactly-once semantics at API level)
- concurrency safety (no double spend) via conditional atomic debit
- double-entry ledger (DEBIT + CREDIT per transfer)
- stored wallet balances updated atomically in the transfer transaction

Design notes: `DESIGN.md`.

## Requirements

- Go `1.24+`
- Docker + Docker Compose

## Run (Docker Compose)

```bash
make up
```

This brings up:

- Postgres
- migrations
- API on `http://localhost:8080`

All containers join the docker network named `kuku_assignment`.

## API

Create wallets:

```bash
curl -s -X POST http://localhost:8080/wallets \
  -H 'content-type: application/json' \
  -d '{"initialBalance":1000}'
```

Get wallet:

```bash
curl -s http://localhost:8080/wallets/<walletId>
```

Create transfer:

```bash
curl -s -X POST http://localhost:8080/transfers \
  -H 'content-type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"<from>","toWalletId":"<to>","amount":100}'
```

Replaying the same request with the same `idempotencyKey` returns the original response and does not duplicate side effects.

## Test

Unit + compile checks:

```bash
make test
```

Integration tests (require a running Postgres):

```bash
export TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable'
go test ./... -run TestTransfer_
```

## Lint / Format

```bash
make fmt-check
make lint
```
