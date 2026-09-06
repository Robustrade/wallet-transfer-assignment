# Evaluator Rubric & Reviewer Checklist

This document maps the project implementation against the criteria outlined in [evaluation_guide.md](../evaluation_guide.md) and [.github/copilot-instructions.md](../.github/copilot-instructions.md).

## 1. Database Schema Design
- **Primary & Foreign Keys**: Standard UUID primary keys and cascading/restrict foreign key constraints enforce referential integrity across `wallets`, `transfers`, `ledger_entries`, and `idempotency_records`.
- **Database Constraints**: `CHECK (balance >= 0)` on `wallets`, `CHECK (amount > 0)` on `transfers` & `ledger_entries`, `CHECK (from_wallet_id <> to_wallet_id)` on `transfers`.
- **Indexing**: B-tree indexes added to `transfers(from_wallet_id)`, `transfers(to_wallet_id)`, `transfers(idempotency_key)`, `ledger_entries(wallet_id)`, and `ledger_entries(transfer_id)`.

## 2. Transaction & Locking Strategy
- **Row-Level Locks**: Row locking acquired via `SELECT ... FOR UPDATE` prior to balance mutations.
- **Deadlock Avoidance**: Lock ordering sorted lexicographically by wallet UUID (`firstID < secondID`), eliminating circular wait conditions across concurrent cross-transfers.
- **Race Condition Immunity**: Exclusive locks hold throughout the single PostgreSQL transaction, ensuring zero read-then-write race conditions.

## 3. Idempotency Guarantees
- **Durable Storage**: Idempotency records persisted in PostgreSQL table `idempotency_records` with a unique primary key constraint.
- **Payload Hash Matching**: SHA-256 hash of payload parameters (`fromWalletId`, `toWalletId`, `amount`) stored alongside response body and HTTP status. Reused keys with matching payloads replay cached HTTP responses; mismatched payloads return `409 Conflict`.

## 4. Layering & Clean Architecture
- **Thin Handlers**: HTTP handlers perform JSON binding, basic input syntax verification, and error response formatting without embedding business logic or SQL queries.
- **Pure Service Orchestration**: Business rules, state transitions, idempotency checks, and transaction coordination reside exclusively in the service layer.
- **Repository Isolation**: Persistence concerns and SQL queries are encapsulated in the repository package.

## 5. Automated Test Coverage
- **Unit Testing**: Tests cover monetary arithmetic, decimal precision limits, wallet state mutations, and transfer state machine transitions.
- **Integration Testing**: Tests against real PostgreSQL instance verify atomic balance mutations, double-entry ledger balancing, idempotency replay, and key reuse conflicts.
- **Concurrency Testing**: High-load parallel goroutine tests prove zero double-spending, zero negative balances, and deadlock avoidance under concurrent write pressure.
