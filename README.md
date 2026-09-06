# Wallet Transfer Assignment Repository

This repository is a reusable coding assignment template for evaluating backend engineers on wallet transfers, idempotency, concurrency control, and double-entry ledger design.

## Solution

This branch contains an implementation of the assignment in Go. See [`DESIGN.md`](./DESIGN.md)
for the API contract, schema, idempotency strategy, and concurrency strategy.

### Stack

- Go 1.24+
- SQLite via [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) — a pure-Go driver, chosen
  over `mattn/go-sqlite3` so the project builds without a cgo toolchain (no C compiler needed locally
  or in CI).

### How to Run

```sh
go run ./cmd/server
# or: make run
```

Listens on `:8080` by default (override with `ADDR`), and creates `wallet_transfer.db` in the
working directory by default (override with `DB_PATH`).

```sh
curl -X POST localhost:8080/wallets -d '{"id":"wallet_1","initialBalance":1000}'
curl -X POST localhost:8080/wallets -d '{"id":"wallet_2","initialBalance":0}'
curl -X POST localhost:8080/transfers -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
curl localhost:8080/wallets/wallet_1
curl localhost:8080/transfers/<transferId>
```

### How to Test

```sh
go test ./... -v
# or: make test
```

Includes unit tests (domain state machine), service-level tests (happy path, idempotent replay,
idempotency key conflict, insufficient funds, wallet/transfer not found), and concurrency tests
(concurrent transfers draining the same wallet, concurrent duplicate requests).

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
