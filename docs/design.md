
# Wallet Transfer Service (PostgreSQL + net/http)

A transactional wallet transfer service implemented using:

- Go
- net/http
- PostgreSQL
- sqlx

No external HTTP framework is used.

---

# Features

- Idempotent transfer processing
- Double-entry ledger
- ACID transactions
- Concurrency-safe balance updates
- Transfer state machine
- Clean layered architecture
- PostgreSQL persistence
- Tests included

---

# Project Structure

```text
cmd/
internal/
  database/
  domain/
  handler/
  repository/
  service/
```

---

# Run PostgreSQL

Example using Docker:

```bash
docker run --name wallet-postgres   -e POSTGRES_PASSWORD=postgres   -e POSTGRES_DB=walletdb   -p 5432:5432   -d postgres:16
```

---

# Run Service

```bash
go mod tidy
go run ./cmd/server
```

Server runs on:

```bash
localhost:8080
```

---

# APIs

## Create Wallet

POST /wallets

```json
{
  "id": "wallet_1",
  "balance": 1000
}
```

---

## Get Wallet

GET /wallets?id=wallet_1

---

## Create Transfer

POST /transfers

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

---

# Concurrency Strategy

Transfers execute inside DB transactions.

Wallet rows are locked using:

```sql
SELECT ... FOR UPDATE
```

This guarantees:
- no double spending
- correct balances
- safe concurrent execution

---

# Idempotency Strategy

- `idempotency_key` has a UNIQUE constraint
- duplicate requests return original transfer
- duplicate side effects are prevented

---

# Ledger Rules

Every transfer creates:
- one DEBIT entry
- one CREDIT entry

Ledger always balances.

---

