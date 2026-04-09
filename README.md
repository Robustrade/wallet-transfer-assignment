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


# Wallet Transfer Service

## Setup

Run the application using Docker:

```bash
docker-compose up --build
```

---

## API

### POST /transfers

Example request:

```bash
curl -X POST http://localhost:8080/transfers \
-H "Content-Type: application/json" \
-d '{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}'
```

---

## Features

* Idempotent transfers using idempotency key
* Double-entry ledger system
* Transaction-safe operations
* Concurrency safety using row-level locking

---

## Design Decisions

* Used PostgreSQL for strong consistency
* Used database transactions to ensure atomicity
* Used `SELECT FOR UPDATE` for concurrency control
* Stored idempotency responses to ensure exactly-once behavior

---

## Notes

* Tables are auto-created on startup
* Sample wallets are pre-inserted
