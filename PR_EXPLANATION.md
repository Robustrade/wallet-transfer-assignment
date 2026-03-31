# Wallet Transfer Service - Pull Request Notes

## Overview
This PR implements the Wallet Transfer Service, adhering to the requirements of reliability, idempotency, concurrent transaction safety, and double-entry ledger tracking.

## 1. Database Schema
- Maintained a standard `wallets` table storing dynamic balances to avoid expensive aggregations on reads.
- `transfers` table includes an `idempotency_key` with a `UNIQUE` constraint to reject duplication at the lowest possible layer.
- `ledger_entries` ensure a double-entry representation with exact atomic mappings (`DEBIT`/`CREDIT`) to the transfer operation.

## 2. Transaction and Locking Strategy
- Ensured total atomic operations using `sql.Tx` with a `ReadCommitted` isolation level.
- Before updating balances, we lock both the Sender and Receiver’s rows using `SELECT ... FOR UPDATE`.
- **Deadlock Prevention**: Wallets are ordered alphabetically by ID before grabbing locks, ensuring two concurrent transfers across the same wallets in opposing directions do not mutually lock each other forever.
- Checked balance logic firmly while locks are held tightly to avoid any `read-then-write` race condition overspending.

## 3. Idempotency Strategy
- **Layer 1 (Fast path)**: The service layer queries the `transfers` table for an existing `idempotency_key`. If found, it fetches and safely returns the existing previously processed result.
- **Layer 2 (Slow concurrent path)**: Under extreme concurrency where the initial check passes, the `INSERT INTO transfers` evaluates the `UNIQUE` DB constraint. If `err == 23505 (unique_violation)`, it gracefully falls back and retrieves the already safely inserted transfer from the twin concurrent routine.
- Duplicate side effects are inherently impossible structurally.

## 4. Code Structure
Followed a simple, highly scalable domain-driven architecture:
*   `internal/model`: Pure struct definitions.
*   `internal/repository`: Interface + pure Postgres driver implementation, isolating SQL logic.
*   `internal/service`: Core business orchestration handling validations and idempotency flows without executing raw SQL.
*   `internal/handler`: Thin net/http wrappers parsing JSON and forwarding strictly mapped error states down the pipe.

## 5. Testing & Dockerization
- Provided Table-Driven unit tests for the Service layer testing input/idempotency routing.
- Provided Deep Integration tests directly interacting with PostgreSQL asserting robust concurrent double-spend blocks & concurrent idempotency races.
- Included `Dockerfile` and `docker-compose.yml` for effortless isolated execution.

## AI Usage Note
I utilized AI tools (Anthropic/Gemini) acting organically as a collaborative pair programming partner during the development of this PR. 
**Specific prompts included:** 
1. "Guide me through end-to-end setting up a production-ready clean architecture in Go for the wallet transfer assignment."
2. "Design PostgreSQL schemas with proper uniquely constrained idempotency logic and foreign keys."
3. "Generate rigorous concurrency test suites mimicking aggressive double-spend and exact-time concurrency idempotency clashes." 

The AI heavily aided the speed of testing boilerplate out, while architectural validation like the precise ordering of DB Row Locks (`SELECT FOR UPDATE`) and deadlock mitigations were deliberately reviewed with standard engineering rigorousness.
