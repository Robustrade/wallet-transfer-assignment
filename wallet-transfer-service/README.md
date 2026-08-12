# Wallet Transfer Service

Go + PostgreSQL implementation for the wallet-transfer coding assignment.

## Planned architecture

Handler -> Service -> Repository -> PostgreSQL

## Local PostgreSQL

```bash
docker compose up -d
```

Default connection:
`postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable`

## Run

```bash
go mod download
go run ./cmd/server
```

Health check:
`GET http://localhost:8080/health`

## Implementation order

1. Migration runner
2. Transaction/repository abstraction
3. Transfer service
4. POST /transfers
5. Unit and PostgreSQL integration tests
6. Concurrency/idempotency tests

## Create transfer

`POST /transfers`

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "<source-uuid>",
  "toWalletId": "<destination-uuid>",
  "amount": 10000
}
```

`amount` is stored in the smallest currency unit. For INR, `10000` means ₹100.00.

Successful transfers return `201 Created`. A transfer that cannot be processed because the source wallet has insufficient funds is persisted as `FAILED` and returns `422 Unprocessable Entity`.

Reusing the same idempotency key with the same request returns the original transfer. Reusing it with different transfer parameters returns `409 Conflict`.


## Testing

The integration suite uses PostgreSQL and exercises the important correctness invariants:

- successful transfer and balance updates
- exactly two ledger entries
- idempotent retries
- idempotency-key conflicts
- insufficient funds
- concurrent spending from the same wallet
- concurrent requests using the same idempotency key
- matching debit/credit ledger amounts

Start PostgreSQL and run:

```bash
docker compose up -d postgres
go test ./internal/integration -v
go test -race ./...
```

If your PostgreSQL connection differs from the default, set `DATABASE_URL`.
