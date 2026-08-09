# Wallet Transfer Assignment Repository

This repository is a reusable coding assignment template for evaluating backend engineers on wallet transfers, idempotency, concurrency control, and double-entry ledger design.

## Included

- `ASSIGNMENT.md` - candidate-facing prompt
- `.github/pull_request_template.md` - required PR structure
- `.github/workflows/ci.yml` - lint, format, test placeholder workflow
- `.github/workflows/sonarqube.yml` - SonarQube pull request analysis
- `.github/copilot-instructions.md` - repository-level Copilot review guidance
- `evaluation_guide.md` - reviewer rubric
- `branch-protection-checklist.md` - GitHub setup checklist

## Intended use

1. Mark this repository as a GitHub template repository.
2. Create one private repository per candidate from the template.
3. Add the candidate as a collaborator.
4. Ask them to submit via a pull request into `main`.
5. Enable required checks, SonarQube, and Copilot review in GitHub.

## Notes

- Copilot automatic pull request review is configured in GitHub repository or organization settings, not purely through files in the repo.
- The `copilot-instructions.md` file included here provides repository-specific review guidance once Copilot review is enabled.
- The CI workflow is language-agnostic by default and expects you to set the `LINT_CMD`, `FORMAT_CHECK_CMD`, and `TEST_CMD` repository variables or replace the commands directly.

## How to Submit Assignment

1. **Fork this repository** to your own GitHub account.
2. Complete the assignment described in [`ASSIGNMENT.md`](./ASSIGNMENT.md).
3. **Raise a Pull Request** back to this repository (`main` branch) with your full solution.

Your PR branch should be named: `solution/<your-name>` (e.g., `solution/jane-doe`).

## Databse migration commands

migrate create -ext sql -dir database/migrations/ wallets_table
migrate create -ext sql -dir database/migrations/ transfers_table
migrate create -ext sql -dir database/migrations/ ledger_entries_table

## unit test
wsl -e bash -lc 'mkdir -p /mnt/c/Users/chait/go/src/wallet-transfer-assignment/internal/repository/mock && mockgen -source=/mnt/c/Users/chait/go/src/wallet-transfer-assignment/internal/repository/interface.go -destination=/mnt/c/Users/chait/go/src/wallet-transfer-assignment/internal/repository/mock/interface.go -package=mock'


# Wallet Transfer Service

## Overview

A Go service for wallet-to-wallet transfers using PostgreSQL.

The service guarantees:

- idempotent transfer requests
- atomic balance updates
- double-entry ledger records
- concurrency-safe wallet debits
- retry-safe transfer processing

## Tech Stack

- Go
- PostgreSQL
- pgx / pgxpool
- sqlc
- golang-migrate
- chi router
- Docker Compose

## Architecture

HTTP Controller
    ↓
Service
    ↓
Repository
    ↓
sqlc / pgx
    ↓
PostgreSQL

### Responsibilities

Controller:
- parses HTTP requests
- maps responses/errors
- calls the service layer

Service:
- validates transfer requests
- handles business rules
- orchestrates repository calls

Repository:
- executes database transactions
- locks wallet rows
- updates balances
- writes ledger entries
- handles transfer persistence

## Database Design

### wallets

Stores wallet balances.

Important fields:

- `id`
- `balance`

Balance is stored directly rather than derived from the ledger.

### transfers

Stores transfer requests and state.

Important fields:

- `id`
- `idempotency_key`
- `from_wallet_id`
- `to_wallet_id`
- `amount`
- `status`

`idempotency_key` has a unique constraint to prevent duplicate transfers.

Allowed states:

- `PENDING`
- `PROCESSED`
- `FAILED`

### ledger_entries

Stores double-entry accounting records.

Each successful transfer creates exactly:

- one `DEBIT` entry for the source wallet
- one `CREDIT` entry for the destination wallet

## Transfer Flow

For a successful transfer:

1. Check whether the idempotency key already exists.
2. Begin a database transaction.
3. Lock source and destination wallets using `SELECT ... FOR UPDATE`.
4. Validate that both wallets exist.
5. Create the transfer in `PENDING` state.
6. Verify the source wallet has sufficient funds.
7. Debit the source wallet.
8. Credit the destination wallet.
9. Insert the `DEBIT` ledger entry.
10. Insert the `CREDIT` ledger entry.
11. Update the transfer to `PROCESSED`.
12. Commit the transaction.

If an unexpected database error occurs, the transaction is rolled back.

## Idempotency Strategy

The idempotency key is stored in the `transfers` table and protected with a unique database constraint.

If the same key is received again, the existing transfer is returned and no balance or ledger updates are repeated.

For this assignment, a separate `idempotency_records` table was not introduced because the service exposes only one idempotent operation: creating a transfer.

For a larger system with multiple idempotent operations, idempotency could be extracted into a shared table/service.

## Concurrency Strategy

Each transfer runs inside a PostgreSQL transaction. Both wallet rows are selected using `SELECT ... FOR UPDATE`, which makes PostgreSQL lock them until the transaction commits or rolls back.

The balance is checked only after acquiring the lock, preventing concurrent transfers from spending the same balance. Wallets are locked in a consistent order using `ORDER BY id` to reduce deadlock risk.

## How to Run

### 1. Start PostgreSQL

```bash
docker compose up -d
```

### 2. Set Database URL

```bash
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/wallet_transfer?sslmode=disable"

migrate -path database/migrations -database "$DATABASE_URL" up

or

make migrate-up
```

### 3. Seed Test Wallets

```bash
make seed
```

### 5. Start the Application

```bash
go run ./cmd/main.go

or 

go build -o transfer .\cmd\main.go
.\transfer
```

The API will be available at:

```text
http://localhost:8080
```

### 6. Test a Transfer

```bash
Use Postman or any other client
```

### 7. Run Tests

```bash
make test
```

### 8. Stop PostgreSQL

```bash
docker compose down
```