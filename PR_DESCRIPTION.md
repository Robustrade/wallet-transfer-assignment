# Wallet Transfer Service

## Overview

Built a wallet-to-wallet transfer service with a single endpoint:

`POST /transfers`

Features:
- Wallet to wallet money transfer
- Idempotent requests using `idempotencyKey`
- Double-entry ledger (DEBIT & CREDIT)
- Database transaction for atomic transfers
- Safe concurrent transfers using row locks
- Transfer status: `PENDING` → `PROCESSED` / `FAILED`

Project structure:
- Handler - HTTP layer
- Service - Business logic
- Repository - Database operations
- Domain - Models & validation

Seed data:
- wallet-alice : 100000
- wallet-bob : 50000

---

## AI Disclosure

AI tool used: Cursor

Used AI only as a coding assistant for:
- Creating the test plan
- Generating test cases
- Debugging failed tests
- Fixing a few bugs
- Writing documentation

The application design, database schema, and transfer logic were implemented by me.

---

## Database Schema

### wallets
- id
- balance
- created_at
- updated_at

### transfers
- id
- idempotency_key
- from_wallet_id
- to_wallet_id
- amount
- status
- created_at
- updated_at

### ledger_entries
- id
- transfer_id
- wallet_id
- entry_type (DEBIT / CREDIT)
- amount

### idempotency_records
- idempotency_key
- transfer_id
- response_body
- http_status
- created_at

Notes:
- Wallet balance is stored directly for faster reads.
- Ledger is maintained for audit.
- Database constraints prevent invalid transfers.
- Indexes added on frequently queried columns.

---

## Idempotency

- Check `idempotency_records` first.
- If found, return the stored response.
- Otherwise create the transfer.
- Duplicate requests with the same key return the original response.
- Success and failure responses are both cached.

---

## Concurrency

- Entire transfer runs inside one database transaction.
- Both wallets are locked using `SELECT ... FOR UPDATE`.
- Wallets are always locked in the same order to avoid deadlocks.
- Balance check happens after acquiring locks.
- Prevents double spending during concurrent requests.

Example:
- Balance = 150
- Two concurrent transfers of 100
- One succeeds.
- One fails.
- Final balance = 50.

---

## Running the Project

Requirements:
- Docker
- Go 1.25+

```bash
make db-up
make migrate-up

cp .env.example .env

go run ./cmd/server
```

Server:

```text
http://localhost:8080
```

Sample request:

```bash
curl -X POST http://localhost:8080/transfers \
-H "Content-Type: application/json" \
-d '{
  "idempotencyKey":"req-001",
  "fromWalletId":"wallet-alice",
  "toWalletId":"wallet-bob",
  "amount":100
}'
```

Run tests:

```bash
go test ./...
```

Tests cover:
- Validation
- Successful transfers
- Ledger entries
- Idempotency
- Error cases
- Concurrent transfers

Detailed test scenarios are available in `TEST_CASES.md`.

---

## Assumptions & Trade-offs

- Wallet balance is stored instead of calculating it from the ledger for faster reads.
- Amounts are stored in whole rupees (`BIGINT`).
- Negative balances are prevented through application logic and row locking.
- No cleanup process for `PENDING` transfers since failed transactions are rolled back.
- No authentication (not required for the assignment).
- Balance and transaction history APIs were optional, so they were not implemented.