# AI Usage & Collaboration Disclosure

This document details how AI was used during the implementation of the Wallet Transfer Service, as requested in [`ASSIGNMENT.md`](../ASSIGNMENT.md#ai-usage).

---

## 1. Tool Used
- **Tool**: Google Antigravity IDE
- **Environment**: Go 1.24, PostgreSQL 16, macOS

---

## 2. Approach: Force Multiplier & Reviewer Model

I used AI strictly as a **productivity force multiplier** while acting as the **architect and lead reviewer**:

- **System Design & Architecture (My Role)**:
  - I established the architecture, transaction boundaries, and data integrity guarantees.
  - I formulated the concurrency strategy (deterministic alphabetical lock ordering in PostgreSQL via `ORDER BY id FOR UPDATE`).
  - I defined database-level guardrails (`CHECK (balance >= 0)`, foreign key rules, double-entry ledger balance).
  - I reviewed every generated diff for correctness, pruned over-engineered suggestions, and ensured idiomatic Go patterns.

- **Speed & Generation (AI Role)**:
  - Scaffolding repetitive boilerplate (DTO structs, JSON tags, error mapping tables).
  - Generating extensive table-driven unit test matrices covering edge cases.
  - Writing automation files (`Makefile`, Docker Compose, bash smoke test script).
  - Formatting documentation and OpenAPI specs.

### Practical Review Examples
1. **Concurrency Lock Ordering**: When discussing deadlock prevention, I ensured we used PostgreSQL's native `ORDER BY id FOR UPDATE` in `LockPair()` rather than relying on application-level sorting, guaranteeing lock ordering directly inside the database engine.
2. **Lean Observability**: The AI initially suggested a full OpenTelemetry SDK with external Jaeger collector daemons. I chose a cleaner, self-contained approach: `X-Request-ID` correlation across HTTP headers, context, and structured `slog` JSON logs.
3. **Database Guardrails**: I mandated the `CHECK (balance >= 0)` database constraint on `wallets` so safety does not depend solely on application logic.

---

## 3. Session Prompts Log

Here are the prompts used across development:

### Planning & Requirements Analysis
- *"Analyze the assignment requirements in ASSIGNMENT.md, evaluation rubric, and existing repository to map out the technical implementation plan."*

### Core Logic & Ledger
- *"Build the core domain transfer engine with double-entry ledger and deterministic row locking for wallets."*
- *"Implement the PostgreSQL repository layer with a transaction manager supporting WithinTx closures."*
- *"Add database migrations with check constraints for non-negative balances and positive amounts."*

### Idempotency
- *"Implement durable idempotency with SHA-256 payload hashing and 200 OK replay header."*
- *"Ensure identical retries return the original transfer payload with Idempotent-Replay: true header."*
- *"Return 409 Conflict if an existing key is reused with a different amount or wallet."*

### Testing & Concurrency
- *"Write table-driven unit tests for transfer request validation covering edge cases (negative amount, self-transfer, empty IDs, boundary values)."*
- *"Add tests for wallet balance calculation and transfer state transitions."*
- *"Implement wallet balance API (GET /wallets/:id) and transfer history endpoint with service tests."*
- *"Write concurrent transfer safety tests verifying no double-spend under simultaneous debits using goroutines and waitgroups."*
- *"Add an idempotency race test where duplicate requests fire at the exact same millisecond to prove only one commits."*

### Observability & Tooling
- *"Add Prometheus metrics at /metrics for request duration, request counts, and transfer outcomes (created, replayed, rejected, failed)."*
- *"Implement healthz (liveness) and readyz (readiness checking db ping) endpoints."*
- *"Implement X-Request-ID correlation middleware and attach it to structured slog logs."*
- *"Set up local docker-compose for PostgreSQL 16, Makefile automation, and bash smoke tests."*
- *"Generate the OpenAPI 3.0.3 specification in api/openapi.yaml documenting all endpoints, schemas, and headers."*
- *"Help me format the PR description to match pull_request_template.md."*

---

## 4. Conclusion

Using AI as a force multiplier allowed me to deliver a clean, production-grade service—with concurrency safety, double-entry bookkeeping, full test coverage, Prometheus metrics, and operational tooling—in a fraction of the time, while maintaining complete architectural and code quality ownership.
