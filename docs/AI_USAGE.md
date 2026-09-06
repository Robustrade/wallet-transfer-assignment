# AI Tooling Usage Disclosure

this document details the AI tools used during the design, development, and testing of the Wallet Transfer Service.

---

## 1. Tooling & Platform

- **AI Assistant**: Antigravity AI (Claude Sonnet 4.6 )
- **Primary Interface**: IDE integration

---

## 2. Methodology & Usage Description

AI assistance was utilized in a structured, senior-engineer pair-programming workflow:

1. **System & Requirements Analysis**:
   - Extracted functional and non-functional requirements from `ASSIGNMENT.md`, `evaluation_guide.md`, and repository configuration files.
   - Identified evaluator edge cases (double-spending, deadlocks, idempotency key reuse,IEEE 754 precision issues).

2. **Architecture & Design**:
   - Authored the system architecture, database schema, transaction boundaries, and state machine transitions in `docs/design.md`.

3. **Implementation & Refactoring**:
   - Generated Go package structures (`cmd/server`, `internal/domain`, `internal/repository`, `internal/service`, `internal/handler`, `internal/middleware`, `internal/database`).
   - Implemented exact monetary fixed-point logic (`shopspring/decimal`), deterministic row-level locking (`SELECT ... FOR UPDATE ORDER BY id`), double-entry ledger creation, and database-backed idempotency.

4. **Test Suite Development & Verification**:
   - Created unit tests for money domain math and state transitions.
   - Created integration tests against PostgreSQL covering transfer lifecycles, balance rollbacks, idempotency deduplication, and key payload conflict errors.
   - Designed parallel goroutine stress tests for double-spending prevention and deadlock avoidance under concurrent transfer requests.

5. **Human Review & Manual Validation**:
   - All AI-generated code, SQL queries, migration scripts, and test suites were reviewed line-by-line for correctness, race conditions, error propagation, and context handling.
   - Automated tests (`go test -v ./...`), race detector (`go test -race ./...`), and Go code quality tools (`gofmt`, `go vet`) were executed locally and validated against a real PostgreSQL engine.

---

## 3. Full AI Session Transcript




* **Local Log Path**: `<appDataDir>/brain/session_id/.system_generated/logs/transcript.jsonl`

