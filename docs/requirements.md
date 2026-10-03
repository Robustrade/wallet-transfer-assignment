# Wallet Transfer Service — Requirements

## 1. Problem Statement

We need a small service that supports **wallet-to-wallet transfers**: moving an
amount from one wallet to another through a `POST /transfers` endpoint.

Every transfer must guarantee:

- **Idempotent request handling** — when an `idempotencyKey` is provided, the API
  gives exactly-once semantics: a repeated request returns the original result
  and never triggers a duplicate transfer.
- **Double-entry ledger recording** — each transfer records exactly two ledger
  entries, a debit on the source wallet and a credit on the destination wallet,
  and the ledger always balances.
- **Correct balance tracking** — wallet balances stay correct under concurrent
  requests.
- **Safe concurrent execution** — transfers that debit the same wallet at the
  same time must not cause double spending.

The service must behave correctly under duplicate requests, retries, network
failures, and partial execution.

## 2. Goals & Success Criteria

### Goals

Demonstrate good engineering decisions in a reliable transactional system,
favouring clarity, correctness, and robustness over feature completeness. The
solution should show:

- correct handling of idempotency, concurrency, ledger consistency, and safe
  state transitions
- sound database design, transaction safety, and retry-safe behavior
- a clean layered architecture
- testing discipline

### Success Criteria

The solution succeeds when every statement below holds.

**Database**
- Duplicate transfers cannot happen accidentally.
- Ledger entries cannot exist without a transfer.
- Constraints prevent invalid states; idempotency keys are unique.
- Tables have useful indexes.

**Transactions and concurrency**
- Concurrent debits on the same wallet are safe: no double spending.
- There is no read-then-write race.
- Correctness the database should enforce is enforced by the database, not by
  application logic alone.
- Insufficient funds are handled clearly.

**Idempotency**
- Sending the same request twice returns the original result with no repeated
  side effects.
- If the first request commits and its response is lost, a retry returns the
  original result.
- Idempotency holds across process restarts because the key is stored durably.

**Code**
- Handlers are thin, business logic lives in the service layer, and
  repositories handle persistence only.
- The design is not over-engineered.

**Tests**
- Tests describe behavior and cover transfer execution, duplicate requests,
  ledger balancing, failed transfers, and concurrency.
- Tests would catch a regression in idempotency or double spending.

## 3. Functional Requirements

### FR-1 Create Transfer

`POST /transfers` moves `amount` from `fromWalletId` to `toWalletId`.

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

A request is rejected with an error, and no transfer is created, when:

- any of the four fields is missing or empty
- a string field contains the NUL character (U+0000)
- `amount` is not a positive integer
- `fromWalletId` equals `toWalletId`
- either wallet does not exist
- the body is larger than 64 KiB

Behavior:

- `idempotencyKey` is required on every request.
- `amount` is a positive integer in the smallest unit of a single currency
  (for example, `100` = 100 cents). There is no currency field.
- The transfer runs synchronously: the response carries its final state,
  `PROCESSED` or `FAILED`.
- A transfer executes atomically.
- A repeated request with the same `idempotencyKey` and the same body returns
  the original result and does not trigger a duplicate transfer.
- A request that reuses an `idempotencyKey` with a different body is rejected
  with an error; the original transfer is unaffected.

### FR-2 Wallet Balances

- The service maintains a balance for every wallet.
- Balances stay correct under concurrent requests.
- A balance never goes below zero. If the source balance is lower than
  `amount`, the transfer is recorded as `FAILED`, with no ledger entries and
  no balance change. Replaying its `idempotencyKey` returns the same `FAILED`
  result.
- Wallets and their opening balances are created by seed data at setup.
  There is no API to create or fund wallets.
- Whether the balance is stored or derived from the ledger is decided in the
  design doc.

### FR-3 Double-Entry Ledger

Every `PROCESSED` transfer produces exactly two ledger entries:

| entry_id | wallet_id | transfer_id | type   | amount |
|----------|-----------|-------------|--------|--------|
| 1        | wallet_1  | T1          | DEBIT  | 100    |
| 2        | wallet_2  | T1          | CREDIT | 100    |

- a DEBIT on the source wallet
- a CREDIT on the destination wallet
- the ledger always balances

### FR-4 Transfer States

A transfer is in one of three states: `PENDING`, `PROCESSED`, `FAILED`.
The only allowed transitions are:

```text
PENDING -> PROCESSED
PENDING -> FAILED
```

- `PROCESSED` and `FAILED` are final; a transfer never leaves them.
- State transitions are safe under retries and duplicates.

### FR-5 Concurrency Safety

Concurrent transfers, including two that debit the same wallet at the same
time, must leave correct balances, no double spending, and consistent ledger
entries. The strategy (transactions, row-level locks, optimistic locking, or
isolation levels) is chosen and justified in the design doc.

### FR-6 Persistence

- PostgreSQL is preferred; SQLite is acceptable. The choice is recorded in an
  ADR.
- Suggested tables: `wallets`, `transfers`, `ledger_entries`,
  `idempotency_records`. The schema may be extended.

## 4. Non-Functional Requirements

### NFR-1 Idempotency

- `idempotencyKey` is stored durably, so duplicate detection survives process
  restarts.
- Keys are unique across the whole service, and the database enforces that
  uniqueness.
- Keys are kept indefinitely; they never expire.
- If the first request commits and its response is lost, a retry with the
  same key returns the original result.
- If two requests with the same key arrive at the same time, the second waits
  until the first finishes, then returns the same result.
- The design doc defines how the key is stored, how duplicates are detected,
  how the original result is returned, and how duplicate side effects are
  prevented. The PR description summarises it.

### NFR-2 Reliability Under Failure

The service may experience duplicate requests, retries, network failures, and
partial execution. It must provide:

- retry-safe operations
- safe state transitions
- explicit transaction boundaries
- no partial effects: a transfer that fails midway leaves no ledger entries
  and no balance change

### NFR-3 Layered Architecture

| Layer         | Responsibilities                                                       |
|---------------|------------------------------------------------------------------------|
| Handler       | request validation, transport mapping, invoking service logic          |
| Service       | business logic, orchestration, idempotency behavior, transfer workflow |
| Repository    | persistence operations, database interaction                           |
| Domain models | entities, state transitions, validation rules                          |

Handlers stay thin, and persistence logic is not mixed with business rules.

### NFR-4 Testing

- Tests cover transfer execution, idempotency behavior, ledger correctness,
  failure scenarios, and concurrency safety.
- Tests check behavior, not implementation details.
- Work follows Red → Blue → Green: write a failing test for the missing
  behavior, implement the smallest correct solution, then refactor safely
  while keeping tests passing.

### NFR-5 Observability

- The service writes structured logs using Go's `log/slog`.
- Each `POST /transfers` request logs its outcome: the idempotency key, plus
  the transfer ID and state when a transfer exists.
- No metrics are exposed.

## 5. Constraints

- **Language:** Go. CI builds with Go 1.24.
- **Time box:** 3–5 hours.
- **CI checks:** lint, format check, and tests must pass. CI runs on a
  self-hosted runner and installs golangci-lint v1.64.8. The repository's
  setup checklist gives these Go commands:
  - lint: `golangci-lint run ./...`
  - format: `test -z "$(gofmt -l .)"`
  - test: `go test ./... -race -cover`
- **Test environment:** `go test ./...` passes with no external service
  running, because the CI workflow starts no database.

## 6. Scope

### In Scope

- `POST /transfers` with the behavior defined in §3 and §4
- Seed data for wallets and their opening balances
- Structured logging (NFR-5)
- Retry-safe workflows, satisfied by running each transfer in a single
  database transaction and by the idempotency key (NFR-1, NFR-2); nothing
  is built beyond that

### Out of Scope

- Creating or funding wallets through the API
- Wallet balance API
- Transfer history API
- Asynchronous transfer processing
- Multiple currencies
- Idempotency key expiry
- Metrics
- Authentication and authorization

## 7. Open Questions & Decisions

### Decisions

Gaps in the source material (ASSIGNMENT.md, evaluation_guide.md) and how they
were resolved.

| #    | Gap | Decision | Applied in |
|------|-----|----------|------------|
| D-1  | Who calls the service is not stated | Not specified; this is an interview assessment, so there is no users section | — |
| D-2  | Exactly-once applies "when an `idempotencyKey` is provided" | The key is required on every request | FR-1 |
| D-3  | Same key reused with a different body | Rejected with an error; the original transfer is unaffected | FR-1 |
| D-4  | Outcome of insufficient funds | Transfer recorded as `FAILED` with no ledger entries and no balance change; a replay returns the same result | FR-2 |
| D-5  | "Every transfer must generate exactly two entries" conflicts with D-4 | The rule applies to `PROCESSED` transfers; `FAILED` transfers have no entries | FR-3 |
| D-6  | Amount type and currency | Positive integer in the smallest unit of a single currency; no currency field | FR-1 |
| D-7  | How wallets are created and funded | Seed data at setup; no wallet API | FR-2, §6 |
| D-8  | Request validation rules | Reject missing or empty fields, same-wallet transfers, and unknown wallets; nothing is stored | FR-1 |
| D-9  | Synchronous or asynchronous processing | Synchronous; the response carries `PROCESSED` or `FAILED` | FR-1 |
| D-10 | Whether the example lifecycle is exhaustive | Only `PENDING -> PROCESSED` and `PENDING -> FAILED`; both end states are final | FR-4 |
| D-11 | When a transfer is `PENDING` | Each transfer runs in one database transaction (wallet updates, transfer row, two ledger entries); `PENDING` exists only inside it and is never committed | §6, design doc |
| D-12 | Idempotency key retention | Kept indefinitely | NFR-1 |
| D-13 | Concurrent requests with the same key | The second waits for the first, then returns the same result | NFR-1 |
| D-14 | Idempotency key scope | Global | NFR-1 |
| D-15 | Observability expectations | Structured logs with `log/slog`; no metrics | NFR-5 |
| D-16 | Whether tests may need a running database | No; `go test ./...` needs no external service | §5 |
| D-17 | Which optional enhancements to build | Retry-safe workflows only, covered by D-11 and NFR-1/NFR-2; balance API, history API, and metrics are out | §6 |
| D-18 | Authentication and authorization | Out of scope | §6 |
| D-19 | Maximum request body size (raised in PR review) | 64 KiB, over 10× the largest valid request; a larger body is rejected | FR-1 |
| D-20 | NUL characters in string fields (raised in PR review) | Rejected as an invalid field, because PostgreSQL text cannot store U+0000 | FR-1 |

### Open Questions

Deferred to the design doc and ADRs:

- PostgreSQL or SQLite, given D-16 (FR-6)
- Stored balance or balance derived from the ledger (FR-2)
- How seeded opening balances keep the ledger balanced (FR-2, FR-3)
- The concurrency strategy and its justification (FR-5)
