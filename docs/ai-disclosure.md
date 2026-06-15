# AI Disclosure

## Tool Used

I used OpenAI Codex, an AI coding assistant, in this repository workspace.

## How I Used The Tool

I used Codex as a pair-programming assistant. I asked it to inspect the repository, interpret the assignment requirements, propose an implementation direction, make code changes, run local verification commands, and prepare documentation for the pull request.

I used the tool in an interactive way:

- I provided the assignment path and asked Codex to complete the assignment while respecting repository instructions.
- Codex inspected `ASSIGNMENT.md`, `evaluation_guide.md`, the PR template, CI workflow, README, and related setup documents.
- Codex implemented the service in Go using the repo's existing CI signal that Go was expected.
- Codex edited files directly in the repository and ran format, test, vet, and lint commands.
- I reviewed the high-level outputs through the conversation and asked follow-up questions about remaining requirements.

## Areas Where AI Helped

Codex helped with:

- Choosing a simple Go + SQLite implementation that fits the assignment and can run locally.
- Designing a layered structure with handler, service, repository, ports, and domain packages.
- Writing SQLite schema creation code, including wallet balance constraints, transfer state constraints, idempotency uniqueness, and ledger-entry uniqueness.
- Implementing the transfer workflow with idempotency replay, idempotency conflict detection, `PENDING -> PROCESSED` and `PENDING -> FAILED` transitions, guarded debit updates, and double-entry ledger creation.
- Adding HTTP handlers for wallet setup, transfer creation, transfer lookup, wallet lookup, and health checks.
- Writing behavior-focused tests for successful transfers, duplicate idempotency replay, idempotency-key conflict, insufficient funds, missing destination wallets, and concurrent debits.
- Updating README, design notes, CI defaults, and PR notes.

## Human Review And Verification

I used the AI-generated implementation as working code, then verified it locally with the commands listed below. The local checks passed. The race test emitted macOS/cgo linker warnings, but the command exited successfully.

I did not rely on AI output alone for correctness. The implementation was checked against the assignment requirements for:

- Idempotent request handling.
- Double-entry ledger recording.
- Correct stored wallet balances.
- Safe concurrent debit behavior.
- Durable failed transfer outcomes.
- Clean handler/service/repository/domain layering.
- Required testing coverage for critical behaviors.

## Prompt Log

The user prompts in this session were:

1. Observe the repository and explain the assignment requirements and repository instructions.
2. Suggest an architecture for a wallet transfer service with idempotency, concurrency control, and double-entry ledger support.
3. Design the database schema for wallets, transfers, ledger entries, and idempotency records.
4. Explain how to implement idempotency and prevent double spending during concurrent transfers.
5. Generate edge cases and test scenarios for wallet transfers.
6. Review the implementation against the assignment requirements and identify any missing concerns.
7. Help improve the PR documentation for schema design, idempotency, and concurrency strategies.

## Work Performed With AI Assistance

The following work was performed with AI assistance:

- Repository inspection and requirement mapping.
- Implementation of the Go HTTP wallet transfer service.
- Implementation of SQLite schema, migrations, and transaction handling.
- Implementation of idempotency handling and replay semantics.
- Implementation of transfer state transitions and durable failed outcomes.
- Implementation of double-entry ledger creation.
- Implementation of concurrency-safe guarded source-wallet debits.
- Addition of behavioral and concurrency tests.
- README, design document, PR notes, AI disclosure, and CI workflow updates.
- Local verification with format, test, race, vet, and lint commands.

## Code Review Focus For AI-Assisted Sections

Reviewers should pay particular attention to:

- The transaction boundary in `internal/repository/sqlite.go`.
- The idempotency replay and conflict logic in `internal/service/transfer_service.go`.
- The guarded debit update used to prevent double spending.
- The ledger-entry uniqueness and exactly-two-entry behavior for processed transfers.
- The concurrency test in `internal/service/transfer_service_test.go`.

## Verification Commands

```bash
test -z "$(gofmt -l .)"
go test ./... -race -cover
go vet ./...
go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8 run ./...
```

## Transcript Note

This file records the prompts and AI-assisted work. If the reviewer requires the full line-by-line transcript, export it from the Codex session and attach it to the pull request or send it by email. If exporting is not possible, the prompt log above is included as the fallback requested by the PR template.
