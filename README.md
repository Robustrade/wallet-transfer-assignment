# Wallet Transfer — Minimal Go Implementation

Run tests:

```bash
go test ./...
```

Run server (creates wallets.db):

```bash
go run .
```

POST /transfers accepts JSON:

```json
{
  "idempotencyKey":"abc",
  "fromWalletId":"wallet_1",
  "toWalletId":"wallet_2",
  "amount":100
}
```

**How to run (quick)**

- Start server: `go run .` (listens on `:8080`).
- Create or seed wallets (SQLite file `wallets.db` created automatically):

```bash
sqlite3 wallets.db "INSERT INTO wallets (id, balance) VALUES ('wallet_1', 1000);"
sqlite3 wallets.db "INSERT INTO wallets (id, balance) VALUES ('wallet_2', 100);"
```

- Send a transfer request:

```bash
curl -s -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}' | jq
```

**Working scenarios & expected behavior**

- **Successful transfer**: valid `fromWalletId`, `toWalletId`, and sufficient balance.
  - Behavior: a `transfers` row is created (`PENDING` -> `PROCESSED`), two `ledger_entries` are written (DEBIT on source, CREDIT on destination), and both wallet balances update atomically.
  - Example outcome: `wallet_1` balance decreases, `wallet_2` increases by the `amount`.

- **Idempotent retry (duplicate request)**: resend the same `idempotencyKey` payload.
  - Behavior: the service returns the original transfer result and does not create duplicate ledger entries or modify balances again.
  - Use case: client retries after a network timeout — exactly-once semantics at API-level when `idempotencyKey` is provided.

- **Insufficient funds**: attempting to debit more than the source balance.
  - Behavior: transfer is marked `FAILED`, balances unchanged, and the API returns a 422 with `insufficient funds`.

- **Concurrent debits on same wallet**: multiple simultaneous requests attempting to debit the same wallet.
  - Behavior: repository uses a conditional `UPDATE ... WHERE balance >= ?` inside a DB transaction to prevent overdrafts; at-most-one successful debit per available funds. On SQLite tests we restricted DB connections to serialize writes; on Postgres use row-level locking or serializable transactions.

**Implementation notes (summary)**

- **Idempotency**: stored in `idempotency_records`. Service inserts a claim (`IN_PROGRESS`) and later updates the record to `COMPLETED` with `transfer_id` on success. Duplicate requests with the same key return the stored result.
- **Ledger**: double-entry ledger in `ledger_entries` (DEBIT + CREDIT) for every successful transfer to ensure accounting correctness.
- **Balances**: balances stored on `wallets.balance` and updated inside the same transfer transaction for fast reads and strong consistency.

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
