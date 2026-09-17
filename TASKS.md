# Project Tasks and Requirements

## Core Requirements
The goal of this assignment is to build a reliable wallet-to-wallet transfer service demonstrating:
1. **Idempotency**: Requests with the same `idempotencyKey` must return the original result safely without triggering side effects twice.
2. **Double-Entry Ledger**: Every transfer creates exactly two entries (a debit and a credit) and the ledger always balances.
3. **Concurrency Safety**: The system handles simultaneous debits from the same wallet without double-spending, maintaining exact balances.
4. **State Transitions**: The transfer lifecycle handles `PENDING`, `PROCESSED`, and `FAILED` states correctly under concurrency and retries.

## Task Breakdown

### Phase 1: Database & Architecture Setup
- [x] Create the PostgreSQL database schema (`wallets`, `transfers`, `ledger_entries`).
- [x] Add constraints for balance `>= 0` and unique `idempotency_key`.
- [x] Setup the project using a clean layered architecture (Domain, Repository, Service, Handler).

### Phase 2: Core Transfer Logic
- [x] Implement the `CreateTransfer` endpoint.
- [x] Establish transactional boundaries with `sql.LevelReadCommitted`.
- [x] Implement pessimistic locking on wallets (`SELECT ... FOR UPDATE`) with deterministic ordering to prevent deadlocks.
- [x] Validate balances and write to `transfers` and `ledger_entries` within a single transaction.

### Phase 3: Idempotency & Edge Cases
- [x] Handle idempotency replays for successful transfers.
- [x] **[FIXED]** Handle idempotency replays for failed transfers (e.g. returning 422 instead of 201).
- [x] **[FIXED]** Ensure concurrent duplicate requests that fail (e.g., due to insufficient funds) return the original failed transfer instead of a 500 error.

### Phase 4: Testing & Observability
- [x] Write happy path integration tests.
- [x] Write idempotency tests (sequential).
- [x] Write concurrency test for simultaneous debits on the same wallet.
- [x] **[FIXED]** Write concurrency test for simultaneous duplicate idempotency requests that result in insufficient funds.
- [x] Verify ledger balance across all test scenarios.
