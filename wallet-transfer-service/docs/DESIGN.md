# Wallet Transfer Service — Design Decisions

Database schema, idempotency, concurrency, assumptions and tradeoffs


# 1. Overview

The Wallet Transfer Service supports wallet-to-wallet transfers with three core correctness requirements: balances must remain correct, concurrent requests must not cause double spending, and every successful transfer must have a balanced double-entry ledger. The design uses Go with PostgreSQL and separates HTTP handlers, business services, repositories, and domain models.


# 2. Database Schema Design

PostgreSQL is the source of truth for wallet balances, transfers, ledger entries, and idempotency records. The design uses four main tables.


## 2.1 Stored wallet balance

The implementation maintains a stored balance rather than calculating it from the complete ledger for every request. This makes balance reads and transfers efficient. The ledger remains the audit trail. Wallet updates and ledger writes are committed in the same PostgreSQL transaction so the two representations cannot be partially committed.

Money is represented as an integer in the smallest currency unit rather than floating point. For INR, 10000 represents ₹100.00. This avoids floating-point rounding problems.


## 2.2 Double-entry ledger

Each successful transfer creates exactly two entries: one DEBIT for the source wallet and one CREDIT for the destination. For a transfer of 10000 paise, the ledger contains one DEBIT of 10000 and one CREDIT of 10000 associated with the same transfer ID.


# 3. Idempotency Strategy

The API accepts an idempotencyKey so that clients can safely retry a request after a timeout or lost response. The key is stored in PostgreSQL and associated with the resulting transfer.


## 3.1 First request

1. The service computes a deterministic request hash from source wallet, destination wallet, and amount.

1. The idempotency key is checked/created inside the same database transaction as the transfer.

1. A new transfer is created and associated with the idempotency record.

1. Wallet updates, ledger entries, transfer state, and the idempotency record are committed atomically.


## 3.2 Retry

If the same key is received with the same request hash, the original transfer is returned. No second balance update, transfer, or ledger entries are created.


## 3.3 Key reused with different parameters

If the key already exists but the request hash differs, the operation is rejected as an idempotency conflict (HTTP 409). This prevents a different business operation from being accidentally associated with an existing key.


## 3.4 Database uniqueness

The idempotency key has a database uniqueness constraint. Application-side lookup alone is insufficient because two identical requests may arrive concurrently; PostgreSQL provides the final guarantee that only one idempotency record wins.


# 4. Concurrency Handling

The principal concurrency risk is double spending. If a wallet has 10000 units and two concurrent requests each attempt to spend 10000, only one may succeed.


## 4.1 Transaction boundary

A transfer executes inside one PostgreSQL transaction:

1. Check/create the idempotency record.

1. Create the transfer in PENDING state.

1. Lock the source and destination wallet rows.

1. Check that the source has sufficient balance.

1. Debit the source and credit the destination.

1. Insert the DEBIT and CREDIT ledger entries.

1. Transition PENDING to PROCESSED.

1. Commit.

If any database operation fails before commit, the transaction rolls back. This prevents partial state such as a wallet debit without its corresponding credit or ledger entries.


## 4.2 Row-level locking

The wallet rows are locked using SELECT ... FOR UPDATE. The lock is held until the transaction completes. Therefore, when two transfers compete for the same wallet, one transaction performs the balance check/update first; the other waits and then sees the committed balance before deciding whether it has sufficient funds.


## 4.3 Deterministic lock ordering

Each transfer touches two wallets. To reduce deadlock risk, the two wallet IDs are ordered deterministically before locks are acquired. Thus A→B and B→A both acquire locks in the same order instead of each transaction holding one lock while waiting for the other.


## 4.4 Why not optimistic locking?

Optimistic locking with a version column would also work, but conflicts would become an application concern: a failed version update would require transaction retry or failure handling. Row-level locking is easier to reason about for this small service because the critical resource—the wallet balance—is explicitly serialized.


## 4.5 Why not SERIALIZABLE?

SERIALIZABLE isolation could also prevent invalid concurrent outcomes, but serialization failures are expected under contention and require transaction retry logic. The chosen design uses PostgreSQL's normal transaction isolation together with explicit row locks, avoiding that additional complexity while directly protecting the rows being modified.


# 5. Assumptions and Tradeoffs


# 6. Correctness Invariants

- One idempotency key maps to at most one transfer.

- A successful transfer creates exactly one DEBIT and one CREDIT ledger entry.

- DEBIT amount equals CREDIT amount.

- The source wallet is debited exactly once.

- The destination wallet is credited exactly once.

- A successful transfer cannot make a wallet balance negative.

- Wallet changes and ledger entries commit atomically.

- Concurrent transfers cannot double-spend the same wallet balance.

- Wallet locks are acquired in deterministic order.

- Retries with the same idempotency key return the original transfer result.


# 7. Summary

The design deliberately uses strong PostgreSQL guarantees instead of unnecessary distributed-system complexity. Transactions provide atomicity, row-level locks serialize competing wallet updates, unique constraints protect idempotency, and the double-entry ledger provides an auditable record. The main tradeoff is that this design is optimized for a PostgreSQL-backed synchronous service; multi-region writes, external payment systems, asynchronous processing, and event-driven workflows would require additional patterns.



| Table | Purpose | Important fields | Constraints / integrity |

| --- | --- | --- | --- |

| wallets | Current wallet state. | id, balance, created_at, updated_at | Primary key; balance must not be negative. |

| transfers | Business transfer and state machine. | id, from_wallet_id, to_wallet_id, amount, status, timestamps | Foreign keys; amount > 0; source != destination; status ∈ PENDING, PROCESSED, FAILED. |

| ledger_entries | Double-entry financial record. | id, transfer_id, wallet_id, type, amount, created_at | Foreign key; amount > 0; type ∈ DEBIT, CREDIT; uniqueness prevents duplicate entries. |

| idempotency_records | Maps client key to the original operation. | key, request_hash, transfer_id, created_at | Unique/primary-key idempotency key; request hash detects different payload reuse. |



| Decision / assumption | Rationale / tradeoff |

| --- | --- |

| Synchronous processing | The complete transfer is executed in one database transaction. There is no external payment provider or asynchronous workflow in scope. |

| PostgreSQL as source of truth | The solution relies on PostgreSQL transactions, constraints, uniqueness, and row-level locking. |

| Stored balance + ledger | Stored balance provides efficient reads; the ledger provides an audit trail. The transaction must update both consistently. |

| Integer money | Amounts use the smallest currency unit to avoid floating-point errors. Multi-currency behavior is out of scope. |

| No external side effects | There are no emails, webhooks, queues, or external payment calls, so a database transaction is sufficient for required side effects. |

| Idempotency retention | The assignment does not define expiry/retention. Records are retained unless a later operational policy is introduced. |

| API-level exactly-once | Exactly-once applies to the business operation when an idempotency key is used; it does not imply exactly-once network delivery. |
