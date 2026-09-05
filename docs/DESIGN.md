# System Design & Architecture — Wallet Transfer Service

This document describes the architecture, concurrency model, transactional guarantees, and idempotency mechanics of the wallet transfer service.

---

## 1. System Invariants

In financial systems, correctness is non-negotiable. The service guarantees four core invariants:

| Invariant | Guarantee | Enforcement Mechanism |
|---|---|---|
| **Zero Overdraft** | A wallet balance can never drop below zero ($balance \ge 0$). | Application pre-check + PostgreSQL `CHECK (balance >= 0)` constraint. |
| **Double-Entry Conservation** | Money is neither created nor destroyed. Every transfer creates exactly one debit and one credit of equal value ($\sum \text{entries} = 0$). | Atomic database transaction + immutable ledger records. |
| **Deadlock Freedom** | Concurrent transfers between the same wallets (even in opposite directions) never deadlock. | Total resource ordering: wallets are always locked in alphabetical order (`ORDER BY id FOR UPDATE`). |
| **Durable Idempotency** | Network retries cannot execute duplicate debits or create orphan ledger rows. | PostgreSQL-backed idempotency records with SHA-256 payload fingerprinting. |

---

## 2. Request Lifecycle & Transaction Boundaries

```text
Client Request: POST /transfers
      │
      ▼
┌─────────────────────────────────────────────────────────────┐
│ 1. HTTP Layer & Middlewares                                 │
│    - Inject / propagate X-Request-ID                        │
│    - Record Prometheus RED metrics (latency, status)        │
│    - Decode & validate DTO (amount > 0, from != to)         │
└─────────────────────────────┬───────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│ 2. Fast-Path Idempotency Check (Read-Only, No DB Locks)     │
│    - Query idempotency_records by key                       │
│    - If exists with SAME payload   ──► Return 200 OK (Replay)│
│    - If exists with DIFF payload   ──► Return 409 Conflict  │
└─────────────────────────────┬───────────────────────────────┘
                              │ (Key is new)
                              ▼
┌─────────────────────────────────────────────────────────────┐
│ 3. Database Transaction (SERIALIZABLE / WithinTx)            │
│                                                             │
│    Step A: Deterministic Lock Acquisition                   │
│      SELECT ... WHERE id IN ($1, $2) ORDER BY id FOR UPDATE │
│      (Guarantees deadlock-free execution under concurrency) │
│                                                             │
│    Step B: Business Validation                              │
│      - Wallets exist?                                       │
│      - Same currency? (source.currency == dest.currency)    │
│      - Sufficient funds? (source.balance >= amount)         │
│                                                             │
│    Step C: Calculate & Update Balances                      │
│      - source.balance -= amount                             │
│      - dest.balance   += amount                             │
│      - UPDATE wallets (both records)                        │
│                                                             │
│    Step D: Append Double-Entry Ledger                       │
│      - INSERT DEBIT  row for source wallet                  │
│      - INSERT CREDIT row for destination wallet             │
│                                                             │
│    Step E: Persist Transfer & Idempotency State             │
│      - INSERT INTO transfers (status: PROCESSED)            │
│      - INSERT INTO idempotency_records (key, hash, response)│
│                                                             │
│    Step F: Commit Transaction Atomically                    │
└─────────────────────────────┬───────────────────────────────┘
                              │
                              ▼
                 Return 201 Created to Client
```

### Why Transaction Scoping Matters
1. **Replay performance**: Already-processed transfers are resolved in Step 2 **before** touching row locks. Retried requests never block active transfers.
2. **All-or-nothing atomicity**: Balance debits, balance credits, ledger rows, transfer records, and idempotency keys commit in a single PostgreSQL transaction. If anything fails (e.g. insufficient funds, database disconnect), the entire transaction rolls back automatically.

---

## 3. Concurrency Control & Deadlock Prevention

### The Problem: Circular Wait Deadlocks
When two transfers run concurrently between the same pair of accounts in reverse directions:
- **Transfer 1**: `wallet_A` ➔ `wallet_B`
- **Transfer 2**: `wallet_B` ➔ `wallet_A`

In a naive implementation that locks the sender first:

```text
Tx 1 (Transfer A -> B):  Acquires Lock on Wallet A  ──(wants Wallet B)──► [WAITING]
Tx 2 (Transfer B -> A):  Acquires Lock on Wallet B  ──(wants Wallet A)──► [DEADLOCK]
```

Both transactions hold one lock and wait for the other. After `deadlock_timeout` (typically 1 second), PostgreSQL detects the cycle and terminates one transaction with error code `40P01`.

### The Solution: Global Lock Ordering
According to Coffman's conditions, deadlocks require a **circular wait**. We eliminate deadlocks by enforcing a **total order** on lock acquisition.

Both wallets are locked in a single SQL query ordered alphabetically by ID:

```sql
SELECT id, currency, balance, created_at, updated_at
FROM wallets
WHERE id IN ($1, $2)
ORDER BY id
FOR UPDATE;
```

### Execution Flow with Deterministic Ordering:

```text
Tx 1 (A -> B):  Requests lock on A, then B  (alphabetical order)
Tx 2 (B -> A):  Requests lock on A, then B  (same alphabetical order!)

Timeline:
1. Tx 1 acquires row lock on Wallet A.
2. Tx 2 requests row lock on Wallet A ──► Blocks cleanly *before* holding any locks.
3. Tx 1 acquires row lock on Wallet B.
4. Tx 1 updates balances, writes ledger, and commits.
5. Tx 1 releases locks on A and B.
6. Tx 2 wakes up, acquires Wallet A, then Wallet B, and completes.

Result: Zero deadlocks. No transaction abortion.
```

---

## 4. Idempotency Protocol

To provide safe retry semantics over unreliable networks, every transfer requires an `idempotencyKey`:

```text
Client POST /transfers (idempotencyKey, from, to, amount)
                     │
                     ▼
       Compute SHA-256 Fingerprint:
       sha256(fromWalletId + ":" + toWalletId + ":" + amount)
                     │
         ┌───────────┴───────────┐
         ▼                       ▼
   [ Key Exists ]         [ Key Not Found ]
         │                       │
   ┌─────┴─────┐                 ▼
   │           │          Execute Transfer inside DB Tx:
   ▼           ▼          1. Lock wallets (ORDER BY id)
[Same Hash] [Diff Hash]   2. Check balance & currency
   │           │          3. Update balances
   ▼           ▼          4. Insert ledger pair (DEBIT/CREDIT)
Return 200 OK  Return 409 5. Insert transfer (status: PROCESSED)
+ Replay Header (Conflict) 6. Insert idempotency_records
(No money moved)          7. Commit Tx ──► Return 201 Created
```

### Edge Cases Handled:
1. **Identical Replay**: Client retries an already completed transfer (e.g. after network timeout). Returns the original response JSON (`200 OK`) with `Idempotent-Replay: true`. No money moves twice.
2. **Payload Conflict**: Client attempts to reuse an existing key with different parameters. Returns `409 Conflict` (`IDEMPOTENCY_KEY_CONFLICT`).
3. **Concurrent Race**: Two identical requests hit the server at the exact same millisecond. One wins the database insert; the second catches the unique key constraint violation (`23505`) and replays the winner's result.

---

## 5. Database Schema & Integrity

```text
  ┌─────────────────┐             ┌──────────────────┐
  │     wallets     │             │    transfers     │
  ├─────────────────┤             ├──────────────────┤
  │ id (TEXT PK)    │◄───┐   ┌───►│ id (UUID PK)     │
  │ currency (TEXT) │    │   │    │ from_wallet_id   │──┐ (FK)
  │ balance (BIGINT)│    │   │    │ to_wallet_id     │──┤ (FK)
  │ created_at      │    └───┼───►│ amount (BIGINT)  │  │
  │ updated_at      │        │    │ currency (TEXT)  │  │
  └─────────────────┘        │    │ status (TEXT)    │  │
         ▲                   │    │ created_at       │  │
         │ (FK)              │    └──────────────────┘  │
         │                   │              │           │
  ┌──────┴──────────┐        │              │ (1:2)     │
  │ ledger_entries  │        │              ▼           │
  ├─────────────────┤        │    ┌───────────────────┐ │
  │ id (BIGSERIAL)  │        │    │idempotency_records│ │
  │ transfer_id (FK)│────────┘    ├───────────────────┤ │
  │ wallet_id (FK)  │             │idempotency_key(PK)│ │
  │ entry_type (TEXT│             │request_fingerprint│ │
  │ amount (BIGINT) │             │transfer_id (FK)   │◄┘
  │ currency (TEXT) │             │response_body(JSON)│
  │ created_at      │             │created_at         │
  └─────────────────┘             └───────────────────┘
```

### Database Constraints:
- **`CHECK (balance >= 0)` on `wallets`**: Enforces zero overdrafts at the storage engine level.
- **`CHECK (amount > 0)` on `transfers`**: Guarantees transfer amounts are strictly positive.
- **`ON DELETE RESTRICT` on `ledger_entries`**: Historical accounting records can never be cascade-deleted.
- **Composite Index `(from_wallet_id, to_wallet_id, created_at DESC)`**: Optimizes wallet transfer history lookups.

---

## 6. Failure Modes & Mitigations

| Failure Scenario | Risk | Mitigation |
|---|---|---|
| **Client disconnects after commit** | Client thinks transfer failed, but money moved. | Client retries with the same `idempotencyKey`. The server returns `200 OK` with the cached transfer details without moving funds again. |
| **High concurrent debits on one wallet** | Overdraft / race condition. | Row-level `FOR UPDATE` lock serializes checks on that wallet. PostgreSQL `CHECK (balance >= 0)` prevents negative values. |
| **Server process killed mid-transaction** | Partial balance updates or orphan records. | PostgreSQL aborts uncommitted transactions. All changes are rolled back by the database engine. |
| **Concurrent bidirectional transfers** | Database deadlock (`40P01`). | Deterministic alphabetical locking (`ORDER BY id FOR UPDATE`) eliminates circular waits. |
| **Cross-currency transfer attempt** | FX rate ambiguity or corrupted values. | Rejected at domain level (`422 Unprocessable Entity`). Multi-currency transfers require an explicit conversion rate service. |

---

## 7. Engineering Tradeoffs & Decisions

### 1. Modular Monolith vs Distributed Microservices
- **Decision**: Clean modular monolith with strict domain/service/repository interfaces.
- **Why?** Intra-ledger money transfers require strict ACID guarantees so that debits, credits, and ledger rows commit atomically. In a distributed microservice architecture (e.g., separate `WalletService`, `LedgerService`, and `TransferService` with independent databases), a single transfer requires distributed transactions (Saga orchestration, Two-Phase Commit, or Outbox pattern with compensating actions). Distributed transactions introduce network latency, partial failure windows, and eventual consistency risks. An ACID database transaction provides absolute consistency, lowest latency, and zero split-brain risk. If scaled across multiple services, the evolutionary path would be an orchestration Saga with temporary balance holds (`HOLD` / `RESERVE`).

### 2. Pessimistic Row Locking vs Optimistic Concurrency
- **Decision**: Pessimistic row locking via `SELECT ... FOR UPDATE` with alphabetical ordering.
- **Why?** Optimistic locking (version numbers + retry loops) works well when collisions are rare. In financial systems with hot wallets (e.g. a merchant or payroll account receiving 50 transfers/second), optimistic locking causes heavy retry storms and wasted CPU. Pessimistic locking queues requests cleanly in PostgreSQL.

### 3. Stored Balances + Ledger vs Pure Ledger Aggregation
- **Decision**: Stored `balance` column on `wallets` combined with immutable `ledger_entries`.
- **Why not pure ledger aggregation (`SUM(amount)`)?** Calculating a balance by summing historical ledger rows is $O(N)$ and degrades as transaction volume grows. Storing the balance provides $O(1)$ reads for balance checks and allows an engine-level `CHECK (balance >= 0)` constraint, while double-entry ledger rows provide a complete, immutable audit trail.

### 4. PostgreSQL Idempotency vs Distributed Redis
- **Decision**: Store idempotency records in PostgreSQL within the transfer transaction.
- **Why not Redis?** Storing idempotency keys in Redis introduces a distributed dual-write problem. If Redis succeeds but PostgreSQL fails to commit, the transfer is lost but can never be retried. Storing both in PostgreSQL guarantees that balance updates, ledger rows, and idempotency status commit or rollback **atomically**.

### 5. Synchronous REST vs Asynchronous Event-Driven Queueing
- **Decision**: Synchronous HTTP endpoint (`POST /transfers`).
- **Why?** Consumer-facing payment flows (P2P transfers, checkout) require immediate confirmation (`201 Created` or `422 Insufficient Funds`). For bulk asynchronous payouts (e.g., batch payroll for 50,000 employees), an event-driven architecture using Kafka or RabbitMQ with worker pools would be layered on top to rate-limit database IO and smooth out traffic spikes.

### 6. Single-Currency Transfers vs Real-Time FX Conversion
- **Decision**: Enforce identical currencies (`source.currency == dest.currency`) and reject mismatches (`422 Unprocessable Entity`).
- **Why?** Foreign exchange (FX) conversion introduces external rate provider dependencies, quote locking with TTLs, slippage tolerance, and multi-currency ledger pairs. Rejecting mismatched currencies keeps the core transfer engine deterministic, reliable, and isolated from third-party API availability.

### 7. Database Sharding & Horizontal Scalability
- **Decision**: Single PostgreSQL primary with read replicas for read-heavy endpoints (`GET /transfers/:id`, `GET /wallets/:id/transfers`).
- **Why?** PostgreSQL easily handles thousands of writes per second with proper pooling. Sharding wallets across different physical database instances cannot be done with simple hash partitioning because a single transfer touches *two* arbitrary wallets that might live on different shards. Cross-shard transfers would require a 2PC coordinator or Saga.

### 8. Correlation IDs (`X-Request-ID`) vs Distributed Tracing Daemons
- **Decision**: Propagate `X-Request-ID` across HTTP headers, Gin context, and structured `slog` JSON logs.
- **Why?** Provides end-to-end request correlation across log aggregators (Datadog, Loki, CloudWatch) without introducing external collector daemons (Jaeger, Tempo) or service mesh overhead.

### 9. Integer Minor Units (Paise/Cents) vs Floating Point
- **Decision**: Store amounts and balances as `int64` minor units.
- **Why?** IEEE 754 floating-point arithmetic introduces rounding inaccuracies that are strictly unacceptable in financial ledgers. Storing amounts in paise/cents completely eliminates rounding drift.
