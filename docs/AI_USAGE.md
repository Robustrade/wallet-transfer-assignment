# AI Usage Disclosure

This document provides a transparent and honest disclosure of how artificial intelligence tools were utilized during the design, implementation, and verification of the Robustrade Wallet Transfer Service.

---

## 1. Tool Used

- **Tool Name**: Antigravity (powered by Google DeepMind Advanced Agentic Coding)
- **Environment**: Antigravity IDE / CLI paired with Go 1.25 on Windows.

---

## 2. Methodology & How AI was Used

AI was used as an interactive pair-programming assistant to accelerate boilerplate generation, structure clean architecture boundaries, and verify edge cases:

1. **Requirements & Architecture Analysis**:
   - Reviewed `ASSIGNMENT.md`, `evaluation_guide.md`, and project constraints to establish the design contract.
   - Formalized database schema requirements, integer paise monetary conventions, idempotency rules, and deterministic concurrency control strategies.

2. **Scaffolding and Layered Architecture**:
   - Generated the project layout (`cmd/api`, `internal/domain`, `internal/repository`, `internal/service`, `internal/handler`, `internal/config`, `migrations`, `scripts`, `tests`, `docs`).
   - Implemented domain models, repository interfaces, and GORM database adapters with parameterized SQL for atomic conditional debits.

3. **Concurrency & Transaction Safety**:
   - Designed the deterministic row-locking ordering (`ORDER BY id ASC`) to eliminate deadlock risks between concurrent opposing transfers.
   - Implemented conditional updates (`UPDATE wallets SET balance = balance - $1 WHERE id = $2 AND balance >= $1`) to ensure double-spending is physically impossible at the database row level.
   - Designed the concurrent idempotency claim pattern with PostgreSQL unique constraint arbitration and safe polling on duplicate collision.

4. **Testing Discipline**:
   - Developed comprehensive unit tests for domain validations, money formatting, HTTP request boundaries, and service logic using an in-memory repository mock.
   - Built a full PostgreSQL integration test suite covering concurrent race conditions, duplicate idempotency key collisions, and transactional rollbacks.

5. **Static Analysis & Build Verification**:
   - Executed `gofmt` code formatting, `go vet` static analysis, and `go test` executions.

---

## 3. Prompts & Interaction Summary

Below is the primary prompt provided for the task:

```text
Project: Robustrade Wallet Transfer Service — Go + PostgreSQL + GORM
1. Objective: Implement the complete wallet-transfer coding assignment from the repository.
2. Technology Stack: Go, standard library net/http, PostgreSQL, GORM, versioned SQL migrations, Docker Compose.
3. Architecture: Clean layered architecture (Handler, Service, Repository, Domain, Config).
4. Database Design: Integer paise (BIGINT / int64), users, wallets, transfers, ledger_entries, idempotency_records.
5. Transfer API: POST /transfers with idempotencyKey, fromWalletId, toWalletId, amount.
6. Transaction Workflow: Single atomic PostgreSQL transaction for idempotency, transfer, debit, credit, ledger, and response.
7. Testing: Unit tests and PostgreSQL integration tests for idempotency, concurrency, and rollback.
8. Documentation: README, DESIGN.md, PR_NOTES.md, AI_USAGE.md.
```

---

## 4. Session Transcript & Traceability

- All actions, tool executions, and file modifications in this session were recorded locally in the Antigravity conversation trajectory.
- The session transcript log is maintained under the local agent trajectory directory:
  `<appDataDir>/brain/<conversation-id>/.system_generated/logs/transcript.jsonl`
- Candidates or reviewers can inspect the full chronological step-by-step transcript or request the export artifact.
