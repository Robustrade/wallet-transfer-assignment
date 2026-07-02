# Implementation Checklist

Quick checkbox tracker for the assignment. For detailed **what + why** guidance on each item, see [`IMPLEMENTATION_GUIDE.md`](./IMPLEMENTATION_GUIDE.md).

## Core API

- [ ] `POST /transfers` endpoint (`idempotencyKey`, `fromWalletId`, `toWalletId`, `amount`)
- [ ] Request validation (required fields, positive amount, different wallets)
- [ ] HTTP error mapping (400 validation, 409 insufficient funds, 500 internal)

## Database Schema (PostgreSQL preferred)

- [ ] `wallets` table (id, balance or derived, timestamps)
- [ ] `transfers` table (id, idempotency_key UNIQUE, from/to wallet, amount, state, timestamps)
- [ ] `ledger_entries` table (id, wallet_id, transfer_id FK, type DEBIT/CREDIT, amount)
- [ ] `idempotency_records` table (key UNIQUE, transfer_id, response payload, status)
- [ ] Primary keys, foreign keys, NOT NULL constraints
- [ ] UNIQUE constraint on `idempotency_key` (prevent duplicate transfers)
- [ ] Indexes on `wallet_id`, `transfer_id`, `idempotency_key`
- [ ] Constraints preventing orphan ledger rows

## Double-Entry Ledger

- [ ] Every transfer creates exactly 2 ledger entries (DEBIT + CREDIT)
- [ ] Debit from source wallet, credit to destination wallet
- [ ] Ledger always balances (debit amount == credit amount)

## Transfer State Machine

- [ ] States: `PENDING` → `PROCESSED` | `FAILED`
- [ ] Safe transitions under retries and duplicates
- [ ] Failed transfers do not mutate balances incorrectly

## Idempotency (exactly-once at API level)

- [ ] Durable storage of `idempotencyKey`
- [ ] Duplicate request returns original result (same transfer, same response)
- [ ] No duplicate side effects on replay
- [ ] Safe across process restarts (DB-backed, not in-memory)
- [ ] Handle: request commits but response lost → replay returns same result

## Concurrency & Transactions

- [ ] Explicit DB transaction boundaries for transfer execution
- [ ] Row-level locks or optimistic locking on wallet debits
- [ ] Prevent double-spend on concurrent debits to same wallet
- [ ] No read-then-write race on balance checks
- [ ] Insufficient funds handled atomically inside transaction

## Architecture (clean layered design)

- [ ] Handler layer — validation, HTTP mapping, thin controllers
- [ ] Service layer — business logic, orchestration, idempotency workflow
- [ ] Repository layer — persistence only, no business rules
- [ ] Domain models — entities, state transitions, validation rules

## Testing (behavioral, not implementation-detail)

- [ ] Transfer execution — happy path
- [ ] Idempotency — duplicate `idempotencyKey` returns same result, no double transfer
- [ ] Ledger correctness — two entries per transfer, balanced debits/credits
- [ ] Failure scenarios — insufficient funds, invalid wallets, bad input
- [ ] Concurrency — simultaneous debits on same wallet (if feasible)
- [ ] Follow Red → Blue → Green discipline

## CI / DevOps

- [ ] `go.mod` + Go project structure
- [ ] `LINT_CMD=golangci-lint run ./...`
- [ ] `FORMAT_CHECK_CMD=test -z "$(gofmt -l .)"`
- [ ] `TEST_CMD=go test ./... -race -cover`
- [ ] Database migrations (e.g. golang-migrate, goose, or sql files)
- [ ] `docker-compose.yml` for local PostgreSQL (optional but helpful)
- [ ] `.env.example` with required env vars

## Documentation & Submission

- [ ] README — how to run, how to test, architecture overview
- [ ] Branch: `solution/<your-name>`
- [ ] PR into `main` with:
  - Schema design explanation
  - Idempotency strategy
  - Concurrency strategy
  - Tradeoffs / assumptions
  - How to run & test
- [ ] AI disclosure (tool used, how used, session transcript or prompts)
- [ ] Topical commits with clear commit messages

## Optional Enhancements (bonus, not required)

- [ ] GET wallet balance API
- [ ] GET transfer history API
- [ ] Structured logging / observability
- [ ] Metrics
- [ ] Retry-safe workflow patterns
