# Wallet Transfer Service — Requirements

**Status:** Requirements, with decisions aligned to the implementation  
**Source:** Wallet Transfer Service coding assignment  
**Scope:** Reliable wallet-to-wallet transfers using a transactional relational database

## 1. Problem Statement

Build an HTTP service that transfers funds between wallets while preventing duplicate execution, overspending, and inconsistent accounting records. The service must remain correct when requests are retried, arrive concurrently, or fail during processing.

Correctness and clarity take priority over additional features.

## 2. Goals

- Accept a wallet-to-wallet transfer through `POST /transfers`.
- Ensure one logical transfer is executed at most once for a given idempotency key.
- Record exactly one debit and one credit ledger entry for every **processed** transfer.
- Maintain correct, nonnegative wallet balances under concurrent requests.
- Apply balance updates, ledger entries, transfer state, and idempotency outcome atomically.
- Define safe transfer states and permitted transitions.
- Make business failures and technical failures distinguishable.
- Provide automated behavioral tests, including concurrency and retry scenarios.

## 3. Scope and Non-Goals

### In scope

- Wallet-to-wallet transfers between existing wallets.
- Persistent wallet balances and a double-entry transfer ledger.
- Transfer lifecycle: `PENDING`, `PROCESSED`, `FAILED`.
- Idempotent handling of duplicate API requests.
- PostgreSQL persistence (SQLite is acceptable under the assignment).
- Layered code organization: handler, service, repository, domain.
- Database transactions, concurrency controls, and automated tests.

### Out of scope (unless added explicitly)

- Authentication, authorization, KYC, and fraud screening.
- Deposits, withdrawals, fees, refunds, or reversals.
- Cross-currency transfers and exchange rates.
- External banking/payment integrations.
- Distributed transactions across multiple databases.
- Event queues and asynchronous processing.
- Wallet creation/funding APIs, balance API, transfer history API, dashboards, and advanced observability.

## 4. Functional Requirements

### FR-01 — Create a transfer

The service **MUST** expose `POST /transfers` with:

```json
{
  "idempotencyKey": "abc12345",
  "fromWalletId": 1,
  "toWalletId": 2,
  "amount": 100
}
```

- `idempotencyKey` **MUST** be present and nonempty for the guaranteed retry-safe behavior in this design.
- Source and destination wallets **MUST** exist and be different.
- `amount` **MUST** be a positive integer in the service's smallest supported currency unit (see assumptions).
- The source wallet **MUST** have sufficient available balance at the time of the locked balance check.
- A successful transfer **MUST** debit the source and credit the destination by the same amount.

### FR-02 — Idempotency

- An idempotency key **MUST** identify at most one logical transfer within its defined scope.
- A repeated request with the same key and same canonical request payload **MUST NOT** execute another transfer.
- Once an outcome is committed, an identical retry **MUST** return the originally recorded HTTP status and response body.
- Reusing a key with a different request payload **MUST** return a conflict response and **MUST NOT** create a new transfer.
- Concurrent requests with the same key **MUST** be coordinated by database-enforced uniqueness and transaction semantics, not only an application-level existence check.
- A transaction rolled back before commit **MUST NOT** leave an idempotency result claiming the transfer completed.

**Guarantee boundary:** “Exactly once” here means at most one committed transfer effect per key plus repeatable committed results; the network cannot guarantee that a client receives a response exactly once.

### FR-03 — Wallet balances

- The system **MUST** maintain accurate balances for all processed transfers.
- Wallet balances **MUST NOT** become negative due to concurrent debits.
- Balance modifications for both wallets **MUST** commit or roll back together.
- Concurrent operations affecting a common wallet **MUST** be serialized or otherwise safely coordinated.

### FR-04 — Double-entry ledger

- Each `PROCESSED` transfer **MUST** have exactly two ledger entries: one `DEBIT` for the source and one `CREDIT` for the destination.
- Both entries **MUST** reference the same transfer ID and have the same positive amount.
- The signed net ledger movement for each processed transfer **MUST** equal zero.
- A `FAILED` transfer **MUST NOT** produce money-moving ledger entries.
- Ledger inserts and balance changes **MUST** be committed in the same database transaction.
- Database constraints **SHOULD** prevent duplicate `(transfer_id, entry_type)` entries and invalid entry types/amounts; transaction-level tests **MUST** verify the two-entry invariant.

### FR-05 — Transfer lifecycle

Allowed states:

```text
PENDING -> PROCESSED
PENDING -> FAILED
```

- New transfers **MUST** begin in `PENDING` within the transfer workflow.
- `PROCESSED` and `FAILED` **MUST** be terminal states.
- A transfer **MUST NOT** be processed or failed twice.
- State transitions **MUST** be protected against concurrent execution, using row locks and/or guarded updates.
- `FAILED` **MUST** capture a defined business failure reason.
- Technical failures that cause the transaction to roll back **MUST NOT** be falsely recorded as committed business failures.

**Selected baseline:** The entire synchronous transfer operation, including initial `PENDING` creation and transition to its final state, executes in one PostgreSQL transaction. Therefore, `PENDING` is an internal intermediate state and is normally not externally visible after commit. Persisted pending work and background recovery are not required by the supplied assignment.

### FR-06 — Atomic persistence

A successful transfer's idempotency record, transfer record, both wallet updates, both ledger entries, and final result **MUST** be committed atomically. Any unexpected database or process failure before commit **MUST** leave no partial money movement.

## 5. Expected Behavior and API Outcomes

| Scenario                                  | Expected behavior                                                   | HTTP status                            |
| ----------------------------------------- | ------------------------------------------------------------------- | -------------------------------------- |
| Valid transfer                            | Process once; return transfer ID and `PROCESSED`                    | `201 Created`                          |
| Same key + same payload, completed        | Return stored original status/body; no side effects                 | Original status                        |
| Same key + different payload              | Reject; no new side effects                                         | `409 Conflict`                         |
| Insufficient balance                      | Persist `FAILED` with reason; no balance or ledger changes          | `409 Conflict`                         |
| Wallet does not exist                     | Roll back; no transfer persisted and key not reserved               | `404 Not Found`                        |
| Invalid amount or same source/destination | Reject without processing                                           | `400 Bad Request`                      |
| Database error before commit              | Roll back entire transaction                                        | `500`/`503` as appropriate             |
| Deadlock / retryable contention           | Retry the whole transaction (3 attempts); error if exhausted        | `500` if exhausted                     |
| Same key, original still in progress      | Reject without executing; client retries shortly                    | `409 Conflict`                         |
| Unknown commit outcome                    | Return saved result if committed; otherwise ask client to retry     | Original status, else `503`            |
| Crash after commit but before response    | Retry returns committed original result                             | Original status                        |

**Contract note:** Status codes above are implementation choices, not codes explicitly mandated by the assignment. `API.md` defines the exact response schemas; invalid requests never reserve an idempotency key.

## 6. Side Effects and Consistency

For a successful transfer of amount `A` from wallet `X` to wallet `Y`:

- `X.balance_after = X.balance_before - A`
- `Y.balance_after = Y.balance_before + A`
- One debit ledger entry of `A` belongs to `X`.
- One credit ledger entry of `A` belongs to `Y`.
- The transfer's final status becomes `PROCESSED`.
- The idempotency outcome is recorded for subsequent retries.

For a committed business failure:

- No wallet balance changes.
- No money-moving ledger entries.
- Transfer status is `FAILED`, with a failure reason.
- The original failure result is available to identical retries.

**Consistency invariants:** No committed partial transfer; no overspending; exactly two balanced entries per processed transfer; no duplicate committed transfer effect for the same idempotency key.

## 7. Concurrency Requirements

- The implementation **MUST** protect concurrent debits against double spending.
- For pessimistic locking, lock both wallet rows in a **consistent global order** (ascending numeric wallet ID order) before reading/updating balances.
- Keep database transactions short; do not perform external network calls while holding wallet locks.
- Handle PostgreSQL deadlocks (`40P01`) and serialization failures (`40001`) with bounded retries of the **whole transaction**, when the previous attempt is known to have rolled back.
- Do not blindly replay a transfer after an ambiguous commit outcome; first resolve it using the idempotency record in the database.
- Database uniqueness constraints **MUST** enforce idempotency even when identical requests arrive simultaneously.

## 8. Failure and Retry Requirements

| Failure point                                   | Required outcome                                                                   |
| ----------------------------------------------- | ---------------------------------------------------------------------------------- |
| Before database transaction begins              | No persisted transfer; request may be retried                                      |
| During transfer before commit                   | PostgreSQL rolls back all uncommitted changes                                      |
| Business rejection (e.g., insufficient balance) | Commit a `FAILED` result without money movement                                    |
| After commit, before client receives response   | Retry finds and returns the committed original result                              |
| Duplicate request races with first request      | Wait/coordinate, then return committed result or safely retry if first rolled back |
| Deadlock or serialization error                 | Roll back and retry entire operation with bounded backoff                          |
| Unknown commit outcome due to connection loss   | Query committed idempotency/transfer state before deciding whether to retry        |

## 9. Data and Persistence Requirements

Preferred database: **PostgreSQL**.

Required logical entities:

- `wallets`: wallet ID, stored balance, timestamps.
- `transfers`: transfer ID, source/destination wallet IDs, amount, status, optional failure reason, timestamps.
- `ledger_entries`: entry ID, transfer ID, wallet ID, type (`DEBIT`/`CREDIT`), amount, timestamps.
- `idempotency_records`: unique key, canonical request hash, transfer association, original response status/body, timestamps.

Expected integrity controls include foreign keys, unique keys, positive amount checks, nonnegative wallet balance checks, and indexes on transfer/ledger relationships. Exact schema and constraint definitions belong in `ARCHITECTURE.md` and migrations.

## 10. Architecture Requirements

Use distinct responsibilities:

- **Handler:** Parse and validate HTTP requests; map application outcomes to HTTP responses.
- **Service:** Coordinate idempotency, transfer workflow, transaction boundaries, and business outcomes.
- **Repository:** Execute PostgreSQL queries, enforce lock ordering, and persist entities within the provided transaction.
- **Domain:** Define wallet/transfer/ledger entities, validation rules, and allowed state transitions without HTTP or database-framework dependencies.

## 11. Observability Requirements

- Log transfer ID, request/correlation ID, status, failure category, and duration.
- Never log secrets, authentication credentials, or unnecessary sensitive customer data.
- Track success/failure counts, duplicate-key conflicts, retry counts, deadlocks, and transaction latency where feasible.
- Errors should distinguish business rejection from retryable infrastructure failures.
- Logs must not claim a transfer succeeded before the database commit is confirmed.

## 12. Testing Strategy and Acceptance Criteria

Behavioral tests **MUST** cover:

1. Successful transfer updates both balances and produces exactly two balanced ledger entries.
2. Repeated same-key/same-payload request returns the original result and does not change balances or ledger counts.
3. Same-key/different-payload request is rejected without side effects.
4. Insufficient funds results in `FAILED` without ledger or balance changes.
5. Concurrent debits cannot overspend a wallet.
6. Concurrent duplicate requests cannot create duplicate transfers.
7. Opposite-direction transfers complete correctly without ledger corruption, including retryable deadlock handling.
8. Injected database failure before commit leaves no partial updates.
9. Retrying after a committed transfer returns the original outcome.
10. Invalid amount and same-wallet transfer are rejected.
11. Terminal state transitions cannot be reversed or repeated.

Prefer PostgreSQL integration tests for concurrency and transaction semantics; in-memory mocks alone cannot establish row-lock correctness. Follow red → implementation → refactor while keeping tests green.

## 13. Documentation-First Delivery Workflow

Before non-trivial implementation:

1. Review this requirements document and record open decisions.
2. Define API contracts and response schemas in `API.md`.
3. Document schema, transaction boundary, lock order, and idempotency flow in `ARCHITECTURE.md`.
4. Define test scenarios and concurrency assertions in `TESTING.md`.
5. Implement code, add/update tests, and verify observability and operational behavior.
6. Explain design decisions, assumptions, and trade-offs in the pull request.

## 14. Submission Requirements

- Create a feature branch from `main`.
- Commit implementation, tests, and relevant documentation.
- Open a pull request targeting `main`.
- In the PR, explain database design, idempotency strategy, concurrency handling, and assumptions/trade-offs.
- Disclose AI tool(s) used, how they were used, and provide the full AI session transcript; if unavailable, provide all prompts used, as requested by the assignment.
- Be prepared to explain the submitted code and decisions during PR review.

## 15. Assumptions and Open Decisions

The following clarify ambiguities in the supplied assignment and should be confirmed or explicitly retained in the PR:

| Topic                     | Proposed decision                                                                             |
| ------------------------- | --------------------------------------------------------------------------------------------- |
| Amount representation     | Positive `int64` in minor currency units; avoid floating-point money                          |
| Currency                  | One implicit currency for all wallets; cross-currency out of scope                            |
| Idempotency scope         | Unique per service/repository (no user/tenant dimension specified)                            |
| Idempotency retention     | Retain records for the assessment; production TTL/archival policy is a separate decision      |
| Same key, changed payload | `409 Conflict`                                                                                |
| Failed transfer retry     | Return original committed `FAILED` outcome for the same key                                   |
| Transfer execution        | Synchronous, single DB transaction                                                            |
| Pending visibility        | Intermediate state inside the transaction, not a durable queue                                |
| Balance model             | Stored wallet balance updated atomically with ledger                                          |
| Missing wallet            | `404`; transaction rolled back, no `FAILED` transfer persisted, key not reserved              |
| Identifier types          | Wallet and transfer IDs are database identity `BIGINT` integers; idempotency keys are 8–64 character strings |
| Retry policy              | Bounded retries for known-rolled-back DB contention; ambiguous commits require reconciliation |
| Response format           | `{"error": "<message>"}` for errors; stored original status/body replayed byte-identically for duplicates |

**Design principle:** An implementation may extend this baseline, but any change must preserve atomicity, idempotency, concurrency safety, and ledger consistency.
