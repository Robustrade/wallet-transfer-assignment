# Requirements & Evaluation Checklist

This checklist audits the implementation against the requirements in [`ASSIGNMENT.md`](../ASSIGNMENT.md) and the reviewer rubric in [`evaluation_guide.md`](../evaluation_guide.md).

---

## 1. Functional Requirements (from ASSIGNMENT.md)

| Requirement | Description | Implementation File | Verification Test | Status |
|---|---|---|---|:---:|
| **Create Transfer** | `POST /transfers` atomically transfers funds | `internal/service/transfer_service.go` | `internal/http/router_test.go` | ✅ Complete |
| **Double-Entry Ledger** | Exactly 2 entries (`DEBIT` source, `CREDIT` destination), balanced to zero | `internal/repository/postgres/ledger_repo.go`<br>`internal/domain/ledger.go` | `internal/domain/domain_test.go`<br>`internal/http/router_test.go` | ✅ Complete |
| **Wallet Balances** | Maintains correct balances under concurrent operations | `internal/repository/postgres/wallet_repo.go` | `internal/service/concurrency_test.go` | ✅ Complete |
| **Transfer States** | State machine with `PENDING`, `PROCESSED`, `FAILED` and safe transitions | `internal/domain/transfer.go` | `internal/domain/domain_test.go` | ✅ Complete |
| **Concurrency Safety** | Deadlock-free row locking (`ORDER BY id FOR UPDATE`), zero double-spending | `internal/repository/postgres/wallet_repo.go` | `internal/service/concurrency_test.go` | ✅ Complete |
| **Durable Idempotency** | Replaying identical request returns original result (`200 OK` + `Idempotent-Replay: true`) | `internal/repository/postgres/idempotency_repo.go` | `internal/http/router_test.go`<br>`scripts/smoke.sh` | ✅ Complete |
| **Idempotency Conflict** | Reusing key with different payload returns `409 Conflict` | `internal/service/transfer_service.go` | `internal/http/router_test.go`<br>`scripts/smoke.sh` | ✅ Complete |
| **Zero Overdraft** | Balances cannot drop below zero; returns `422 Unprocessable Entity` | `internal/domain/wallet.go`<br>`migrations/0001_init.sql` (`CHECK (balance >= 0)`) | `internal/domain/domain_test.go`<br>`internal/service/concurrency_test.go` | ✅ Complete |
| **Persistence** | PostgreSQL 16 database with migrations | `migrations/0001_init.sql` | `internal/repository/postgres/db.go` | ✅ Complete |

---

## 2. Optional Enhancements (from ASSIGNMENT.md)

| Enhancement | Description | Implementation File | Verification Test | Status |
|---|---|---|---|:---:|
| **Wallet Balance API** | `GET /wallets/:id` returns wallet balance and currency | `internal/http/handlers/wallet_handler.go` | `internal/http/router_test.go` | ✅ Complete |
| **Transfer History API** | `GET /wallets/:id/transfers` with pagination limit | `internal/http/handlers/wallet_handler.go` | `internal/http/router_test.go` | ✅ Complete |
| **Prometheus Metrics** | `GET /metrics` exposes request latency, counts, and transfer outcomes | `internal/metrics/metrics.go` | `internal/http/router_test.go` | ✅ Complete |
| **Health Probes** | `GET /healthz` (liveness) and `GET /readyz` (readiness pinging DB) | `internal/http/router.go`<br>`internal/repository/postgres/db.go` | `internal/http/router_test.go` | ✅ Complete |
| **Request Tracing** | `X-Request-ID` correlation across headers, context, and structured `slog` | `internal/http/middlewares/middleware.go` | `internal/http/router_test.go` | ✅ Complete |
| **OpenAPI Specification** | OpenAPI 3.0.3 YAML documenting all endpoints and schemas | `api/openapi.yaml` | Validated OpenAPI 3.0 spec | ✅ Complete |

---

## 3. Reviewer Rubric Audit (from evaluation_guide.md)

### 1. Database Schema
- [x] Sensible table design with primary and foreign keys (`ON DELETE RESTRICT`).
- [x] Uniqueness constraints on idempotency keys.
- [x] Check constraint `CHECK (balance >= 0)` on `wallets` strictly prevents negative balances.
- [x] Check constraint `CHECK (amount > 0)` on `transfers` and `ledger_entries`.
- [x] Composite indexes on `transfers` and `ledger_entries` for fast pagination queries.

### 2. Transaction & Locking Strategy
- [x] Explicit transaction boundaries managed via `WithinTx` closure.
- [x] Deterministic alphabetical lock order via `SELECT ... WHERE id IN ($1, $2) ORDER BY id FOR UPDATE` eliminates deadlocks.
- [x] Concurrency safety validated by parallel goroutine tests (`TestConcurrentDebitsCannotOverdraw`).
- [x] Double spending is prevented by both application domain checks and database check constraints.

### 3. Idempotency Strategy
- [x] Durable storage in `idempotency_records` table across server restarts.
- [x] SHA-256 fingerprinting guarantees payload integrity.
- [x] Replay requests return `200 OK` with `Idempotent-Replay: true` header and zero side effects.
- [x] Race condition safety: concurrent duplicate requests receive `409 Conflict` retryable error.

### 4. Layering & Code Quality
- [x] Handlers are thin (<100 lines) and deal only with transport/mapping.
- [x] Business logic lives in domain models and services.
- [x] Repositories handle only SQL and persistence concerns.
- [x] Zero business logic leaked into HTTP handlers or database migrations.

### 5. Testing Discipline
- [x] Unit tests for domain models, validation edge cases, and state transitions.
- [x] Integration tests for HTTP routing, error classification, and middleware.
- [x] Concurrency race tests for simultaneous debits and duplicate idempotency claims.
- [x] Automated end-to-end smoke test script (`scripts/smoke.sh`).
- [x] All tests pass under the Go race detector (`make test-race`).

