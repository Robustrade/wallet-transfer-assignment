# Functional and Non-Functional Requirements Verification Checklist

Every requirement specified in `ASSIGNMENT.md` and project prompt has been implemented and tested as documented below.

| Requirement | Implementation Component | Test Verification | Status |
| :--- | :--- | :--- | :--- |
| **1. Create Transfer Endpoint** (`POST /transfers`) | [transfer_handler.go](../internal/handler/transfer_handler.go) | [transfer_test.go](../tests/integration/transfer_test.go#L18) | ✅ VERIFIED |
| **2. Durable Idempotency** | [idempotency_repo.go](../internal/repository/postgres/idempotency_repo.go) | [idempotency_test.go](../tests/integration/idempotency_test.go#L17) | ✅ VERIFIED |
| **3. Idempotency Key Reuse Conflict** | [transfer_service.go](../internal/service/transfer_service.go#L95) | [idempotency_test.go](../tests/integration/idempotency_test.go#L68) | ✅ VERIFIED |
| **4. Double-Entry Ledger** | [ledger_repo.go](../internal/repository/postgres/ledger_repo.go) | [transfer_test.go](../tests/integration/transfer_test.go#L53) | ✅ VERIFIED |
| **5. Wallet Balances** | [wallet_repo.go](../internal/repository/postgres/wallet_repo.go) | [transfer_test.go](../tests/integration/transfer_test.go#L44) | ✅ VERIFIED |
| **6. Non-negative Balance Enforcement** | DB constraint `CHECK (balance >= 0)` & [wallet.go](../internal/domain/wallet.go#L30) | [domain_test.go](../tests/unit/domain_test.go#L29) | ✅ VERIFIED |
| **7. Concurrency Double-Spend Prevention** | Deterministic `SELECT FOR UPDATE ORDER BY id` | [concurrency_test.go](../tests/integration/concurrency_test.go#L18) | ✅ VERIFIED |
| **8. Deadlock Avoidance** | Sorted UUID locking order | [concurrency_test.go](../tests/integration/concurrency_test.go#L84) | ✅ VERIFIED |
| **9. Money Precision (No Floats)** | `shopspring/decimal` & `NUMERIC(20,4)` | [money_test.go](../tests/unit/money_test.go#L13) | ✅ VERIFIED |
| **10. Transfer State Machine** | `PENDING`, `PROCESSED`, `FAILED` | [domain_test.go](../tests/unit/domain_test.go#L42) | ✅ VERIFIED |
| **11. Get Wallet Balance API** (`GET /wallets/:id/balance`) | [wallet_handler.go](../internal/handler/wallet_handler.go#L53) | Integration API call | ✅ VERIFIED |
| **12. Get Transfer API** (`GET /transfers/:id`) | [transfer_handler.go](../internal/handler/transfer_handler.go#L56) | Integration API call | ✅ VERIFIED |
| **13. Get Wallet Transactions API** (`GET /wallets/:id/transactions`) | [wallet_handler.go](../internal/handler/wallet_handler.go#L76) | Integration API call | ✅ VERIFIED |
| **14. Health Liveness & Readiness Probes** | [health_handler.go](../internal/handler/health_handler.go) | HTTP integration call | ✅ VERIFIED |
| **15. Structured Logging & Request IDs** | [logging.go](../internal/middleware/logging.go) & `log/slog` | Server execution log inspection | ✅ VERIFIED |
| **16. Graceful Shutdown** | [main.go](../cmd/server/main.go#L78) | OS signal handler | ✅ VERIFIED |
| **17. Docker & Docker Compose** | Multi-stage [Dockerfile](../Dockerfile) & compose | `docker compose up` | ✅ VERIFIED |
| **18. Versioned Database Migrations** | `golang-migrate` & [000001_init_schema.up.sql](../migrations/000001_init_schema.up.sql) | Auto-migration at startup | ✅ VERIFIED |
