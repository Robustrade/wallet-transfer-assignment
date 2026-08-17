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

## AI usage
Each line of the code is written from scratch and by author and no coding agent is used. However I have used Chatgpt and Google search for research purposes.

## Database Schema Design
There are 3 tables are created:
1. `wallets`: Each wallet will have `id` and `balance` columns along with `created_at` and `updated_at` columns. `id` is set as primary key hence it will be unique and indexed by postgres.

|Column Name| Data Type| Constraint|
|-----------|-----------|-----------
| id        | VARCHAR(40)| Primary Key|
|balance    | BIGINT |
|created_at | TIMESTAMP|
|updated_at |TIMESTAMP|

2. `transfers`

|Column Name| Data Type| Constraint|
|-----------|-----------|-----------
| id        | VARCHAR(40)| Primary Key|
|from_wallet_id    | VARCHAR(40) |NOT NULL REFERENCES wallets(id)|
|to_wallet_id | VARCHAR(40)|NOT NULL REFERENCES wallets(id)|
|amount |BIGINT| NOT NULL CHECK (amount > 0)|
|status |VARCHAR(20)| NOT NULL CHECK(status IN ('PENDING', 'PROCESSED','FAILED'))|
|idempotency_key |VARCHAR(255)| NOT NULL UNIQUE|

3. `ledger_entries`

|Column Name| Data Type| Constraint|
|-----------|-----------|-----------
| id        | VARCHAR(40)| Primary Key|
|transfer_id    | VARCHAR(40) |NOT NULL REFERENCES transfers(id)|
|wallet_id | VARCHAR(40)|NOT NULL REFERENCES wallets(id)|
|entry_type |BIGINT| NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT'))|
|amount |BIGINT| NOT NULL CHECK (amount > 0)|

## Idempotency Key Strategy
* Clients required to pass idempotency keys in request body else request will be rejected
* Idempotency key should be unique for each new transfer request and if client passes the duplicate 
  key due to retry or user behavior request will be served with already cached result and no duplicate processing will be done

## Concurrency Handling
* Transfer between wallets are used using database transactions so the whole process is atomic by nature.
* By using transactions, we would prevent inconsistent state of the wallet, ledger and transfer tables.
* As `SELECT` statement doesn't lock row, `FOR UPDATE` clause is used while selecting wallets for update.



