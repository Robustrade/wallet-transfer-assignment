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

## Database implementation status

The SQLite persistence layer initializes its schema on startup and provides wallet creation and balance lookup, transactional transfers, double-entry ledger writes, and a dedicated `idempotency_records` table. The idempotency table is the canonical request-key record and stores the original request payload, status, failure reason, and (only after success) the resulting transfer ID.

Idempotency states are `PROCESSING`, `RETRYABLE_FAILED`, and `COMPLETED`. An insufficient-funds attempt is committed as `RETRYABLE_FAILED` with reason `INSUFFICIENT_FUNDS` and no transfer or ledger entries. A later request with the same key and identical payload re-evaluates the current balance. Once a transfer succeeds, the record becomes `COMPLETED`; all later retries return that same transfer. Reusing a key with a different payload is rejected.

Concurrency is protected at the database layer, not with a process-local Go mutex. SQLite writes are serialized through a single configured connection, each transfer runs in a transaction, and the source debit uses a conditional SQL update (`balance >= amount`) so the database itself prevents overdrafts. Transfer, wallet updates, two ledger rows, and the completed idempotency record commit atomically. Concurrent requests using the same key therefore produce one transfer and one pair of ledger entries; concurrent different-key debits cannot spend more than the available balance.

Balances are stored as integer minor currency units (for example, paise or cents). The HTTP handler remains a TODO in this branch; the persistence and service-layer work does not make `POST /transfers` fully operational yet.

> **Existing local database:** This schema changes the idempotency table shape. If you are upgrading from an earlier ZIP, stop the service and remove the old `wallet.db` (and its `-shm` / `-wal` files) once before starting this version. The database is recreated from the embedded schema. Do not do this if you need to preserve existing data.
