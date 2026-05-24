# Wallet Transfer Assignment Repository

This repository is a reusable coding assignment template for evaluating backend engineers on wallet transfers, idempotency, concurrency control, and double-entry ledger design.

## Included

- `ASSIGNMENT.md` - candidate-facing prompt
- `.github/pull_request_template.md` - required PR structure
- `.github/workflows/ci.yml` - lint, format, test workflow
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

---

## Solution — Prathap Chandra

> This section covers the implemented solution on the `solution/prathap-chandra` branch.

### Stack

- **Runtime**: Node.js 20 / TypeScript 5
- **HTTP**: Express 4
- **Database**: PostgreSQL 16 (via `pg`)
- **Tests**: Jest 29 + Supertest
- **Validation**: Zod

### Prerequisites

- Node.js ≥ 18
- Docker + Docker Compose (for PostgreSQL)

### Setup & Run

```bash
# 1. Install dependencies
npm install

# 2. Start PostgreSQL
docker compose up -d postgres

# 3. Copy environment config
cp .env.example .env

# 4. Apply migrations and start server
npm run migrate
npm run dev
```

Server listens on `http://localhost:3000`.

### API

#### Create a transfer

```bash
POST /transfers
Content-Type: application/json

{
  "idempotencyKey": "unique-client-key",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Response `201` on success, `200` on duplicate key (idempotent replay), `422` on insufficient balance, `404` on missing wallet.

#### Get wallet balance

```bash
GET /wallets/:id
```

#### Get wallet ledger

```bash
GET /wallets/:id/ledger
```

### Run Tests

```bash
# Start the test database (port 5433)
docker compose up -d postgres-test

# Run all tests
TEST_DATABASE_URL=postgresql://wallet:wallet@localhost:5433/wallet_transfer_test npm test
```

Tests cover: happy path, idempotency, concurrent debits, insufficient balance, request validation.

### Architecture

```
src/
  handler/      ← Express route handlers (thin: validate → call service → respond)
  service/      ← Business logic, idempotency orchestration, state machine
  repository/   ← SQL queries and database mapping
  domain/       ← Types, custom error classes
  db/           ← Connection pool, migration runner
```

See [`APPROACH.md`](./APPROACH.md) for detailed design decisions.
