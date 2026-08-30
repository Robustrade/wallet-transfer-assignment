# Wallet Transfer Service

Go service for wallet-to-wallet transfers with PostgreSQL. See [ASSIGNMENT.md](ASSIGNMENT.md) and [DESIGN.md](DESIGN.md).

## Run locally

```bash
docker compose up -d
export DATABASE_URL='postgres://wallet:wallet@localhost:5433/wallet?sslmode=disable'
go run ./cmd/server
```

Health check: `GET http://localhost:8080/healthz`

### Create wallets and transfer

```bash
curl -sS -X POST http://localhost:8080/wallets \
  -H 'Content-Type: application/json' \
  -d '{"id":"wallet_1","balance":100}'

curl -sS -X POST http://localhost:8080/wallets \
  -H 'Content-Type: application/json' \
  -d '{"id":"wallet_2","balance":0}'

curl -sS -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'

curl -sS http://localhost:8080/wallets/wallet_1
```

`HTTP_ADDR` defaults to `:8080`. Amounts are integers (no floats).

## Tests

Tests start an embedded PostgreSQL process. You can point them at an existing database with `TEST_DATABASE_URL`.

```bash
go test ./... -count=1
```

## Lint and format

```bash
gofmt -w .
golangci-lint run ./...
```

Expected CI repository variables:

- `LINT_CMD`: `golangci-lint run ./...`
- `FORMAT_CHECK_CMD`: `test -z "$(gofmt -l .)"`
- `TEST_CMD`: `go test ./... -count=1`
