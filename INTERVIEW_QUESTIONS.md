# Wallet Transfer Service — Interview Questions & Answers

This document is derived from the **Wallet Transfer Service** codebase (`com.wallettransfer`). Questions are ordered **easy → hard** and include model answers an interviewer can use to evaluate a candidate who implemented or reviewed this project.

---

## How to use this document

- **Easy** — verifies basic familiarity with the project, stack, and APIs.
- **Medium** — probes design decisions around transactions, idempotency, schema, and layering.
- **Hard** — tests deep understanding of concurrency, failure modes, tradeoffs, and production readiness.

Each question includes:
- **Question** — what to ask
- **Expected answer** — what a strong candidate should cover
- **Code reference** — where to look in the repo (optional follow-up)

---

## Section 1: Easy

### Q1. What does this service do at a high level?

**Expected answer:**  
It is a Spring Boot REST service for **wallet-to-wallet money transfers**. It supports creating transfers with **idempotency**, maintains **wallet balances**, records a **double-entry ledger** (DEBIT + CREDIT), and exposes APIs for balance lookup and transfer history. It is designed for correctness under **concurrent requests** and **duplicate delivery**.

**Code reference:** `README.md`, `ASSIGNMENT.md`

---

### Q2. What technology stack is used?

**Expected answer:**

| Layer | Technology |
|-------|------------|
| Language | Java 21 |
| Framework | Spring Boot 3.3.7 |
| Persistence | Spring Data JPA / Hibernate |
| Databases | H2 (local/test), PostgreSQL (optional profile) |
| API docs | SpringDoc OpenAPI (Swagger UI) |
| Build | Maven |
| Testing | JUnit 5, MockMvc, AssertJ |
| Other | Lombok, Actuator, JaCoCo |

**Code reference:** `pom.xml`, `application.yml`

---

### Q3. What are the main API endpoints?

**Expected answer:**

| Method & Path | Purpose |
|---------------|---------|
| `POST /v1/transfers` | Create an idempotent wallet transfer |
| `POST /v1/wallet-balance` | Get current balance for a wallet |
| `POST /v1/transfer-history` | Get transfer history for a wallet |

Base URL: `http://localhost:8080/api/wallet-transfer`

**Code reference:** `TransferController.java`, `WalletController.java`, `README.md`

---

### Q4. Describe the layered architecture.

**Expected answer:**

```
Controller → Service → Repository → Entity
```

- **Controllers** — HTTP handling, input validation (`@Valid`), Swagger annotations; no business logic.
- **Services** — orchestration: transfers, idempotency, locking, ledger writes.
- **Repositories** — JPA persistence and custom queries (including pessimistic locks).
- **Entities** — schema mapping, DB constraints, safe state transitions.

Supporting packages: `dto`, `enums`, `exception`, `utility`, `builder`, `configuration`.

**Code reference:** `README.md` (Architecture section)

---

### Q5. What fields are required to create a transfer?

**Expected answer:**

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Validation rules:
- `idempotencyKey`, `fromWalletId`, `toWalletId` — required, non-blank
- `amount` — required, minimum 1
- `fromWalletId` must not equal `toWalletId` (checked in service layer)

**Code reference:** `CreateTransferRequestDTO.java`, `TransferService.validateTransferRequest()`

---

### Q6. What database tables exist and what is each one for?

**Expected answer:**

| Table | Purpose |
|-------|---------|
| `wallets` | Stored balance per wallet |
| `transfers` | Transfer workflow and status (`PENDING`, `PROCESSED`, `FAILED`) |
| `ledger_entries` | Double-entry DEBIT/CREDIT per successful transfer |
| `idempotency_records` | Cached API responses keyed by idempotency key + request hash |

**Code reference:** `Wallet.java`, `Transfer.java`, `LedgerEntry.java`, `IdempotencyRecord.java`

---

### Q7. What are the transfer states and allowed transitions?

**Expected answer:**

States: `PENDING`, `PROCESSED`, `FAILED`

Allowed transitions:
```
PENDING → PROCESSED   (successful debit/credit + ledger entries)
PENDING → FAILED      (e.g. insufficient funds; no ledger entries)
```

State changes are enforced on the entity via `markProcessed()` and `markFailed()`, which throw `IllegalStateException` if called from a non-`PENDING` state.

**Code reference:** `TransferStatus.java`, `Transfer.java`

---

### Q8. What happens on a successful transfer in terms of ledger entries?

**Expected answer:**  
Exactly **two** ledger entries are created:
1. **DEBIT** on the source wallet
2. **CREDIT** on the destination wallet

Both entries have the same `amount` and reference the same `transfer_id`. Failed transfers create **no** ledger entries.

**Code reference:** `TransferExecutor.createLedgerEntries()`, `TransferIntegrationTest.shouldProcessTransferAndKeepLedgerBalanced()`

---

### Q9. How do you run the application and tests locally?

**Expected answer:**

```bash
# Run app (H2/local profile)
mvn spring-boot:run

# Run with PostgreSQL
mvn spring-boot:run -Dspring-boot.run.profiles=postgres

# Run tests
mvn test
```

Swagger UI: `http://localhost:8080/api/wallet-transfer/swagger-ui.html`

**Code reference:** `README.md`

---

### Q10. What is the standard API response shape?

**Expected answer:**

```json
{
  "status": "SUCCESS" | "FAIL",
  "message": "...",
  "responseCode": 20001,
  "data": { ... }
}
```

Success codes are in the `20000` range (e.g. `20001` transfer success). Failure codes include `5002` insufficient funds, `5003` idempotency conflict, `5113` wallet not found, `6017` invalid request.

**Code reference:** `ResponseDTO.java`, `ResponseConstant.java`, `ResponseBuilderFactory.java`

---

## Section 2: Medium

### Q11. How is idempotency implemented end-to-end?

**Expected answer:**

1. **Hash the request payload** — SHA-256 over `fromWalletId|toWalletId|amount` (idempotency key is *not* part of the hash).
2. **Lookup `idempotency_records`** by key — if found, verify hash matches; return cached response or throw conflict (`5003`) if hash differs.
3. **Fallback to `transfers` table** — if no idempotency record yet, look up transfer by idempotency key; if `PENDING`, wait/poll until terminal state; rebuild response.
4. **New transfer** — execute in `TransferExecutor`; unique DB constraints on `transfers.idempotency_key` and `idempotency_records.idempotency_key` prevent duplicate side effects.
5. **On `DataIntegrityViolationException`** — retry lookup (another concurrent request won the race).

Same key + same payload → original result (success or failure). Same key + different payload → `5003` conflict.

**Code reference:** `TransferService.java`, `IdempotencyResponseLookup.java`, `TransferExecutor.java`, `RequestHashUtility.java`

---

### Q12. Why is there both an `idempotency_records` table and a unique constraint on `transfers.idempotency_key`?

**Expected answer:**

They serve complementary roles:

- **`transfers.idempotency_key` (unique)** — prevents two transfer rows for the same client request; enforces at-most-one transfer side effect at the DB level.
- **`idempotency_records`** — stores the **full cached HTTP response** (status, code, message, serialized payload) so duplicates can be replayed quickly without recomputing ledger lookups.

The transfer table alone cannot return the exact original API response (especially for failures) as efficiently; the idempotency record is the fast path for replay.

**Code reference:** `IdempotencyRecord.java`, `Transfer.java`

---

### Q13. How does the service prevent double spending under concurrency?

**Expected answer:**

1. **Pessimistic write locks** (`PESSIMISTIC_WRITE`) on source and destination wallet rows via `findByWalletIdsForUpdate`.
2. **Consistent lock ordering** — wallet IDs are sorted before locking to avoid deadlocks (e.g. transfer A→B and B→A lock in same order).
3. **Single transaction** — balance check, debit/credit, ledger writes, and status update happen inside one `@Transactional` boundary.
4. **Optimistic `@Version`** on `Wallet` as a secondary guard against lost updates.
5. **DB check constraint** `balance >= 0` as a last line of defense.

Integration test `shouldPreventDoubleSpendingUnderConcurrentTransfers` verifies only one of two concurrent 100-unit debits from a 100-balance wallet succeeds.

**Code reference:** `WalletRepository.java`, `TransferExecutor.java`, `Wallet.java`, `TransferIntegrationTest.java`

---

### Q14. Why are wallets locked in sorted ID order?

**Expected answer:**  
To prevent **deadlock**. If Thread 1 locks wallet A then B, and Thread 2 locks B then A, both can block forever. Sorting wallet IDs ensures all transactions acquire locks in the same global order.

Example: transfer `wallet_2 → wallet_1` and `wallet_1 → wallet_2` both sort to `[wallet_1, wallet_2]` before locking.

**Code reference:** `TransferExecutor.executeTransfer()` lines 57–63

---

### Q15. Why does `TransferExecutor` use `Propagation.REQUIRES_NEW`?

**Expected answer:**  
Each transfer execution runs in its **own new transaction**. This matters because:

- A failed or rolled-back outer transaction should not roll back a committed idempotency lookup in a separate read transaction.
- Concurrent duplicate requests each get isolated write transactions; the loser hits unique constraint violations and falls back to cached lookup.
- Idempotency lookups use `REQUIRES_NEW, readOnly = true` for similar isolation — reads see committed state from other threads.

**Code reference:** `TransferExecutor.java`, `IdempotencyResponseLookup.java`

---

### Q16. What database constraints protect data integrity?

**Expected answer:**

| Constraint | Where | Purpose |
|------------|-------|---------|
| `balance >= 0` | `wallets` | No negative balances |
| `amount > 0` | `transfers`, `ledger_entries` | Positive amounts only |
| `from_wallet_id <> to_wallet_id` | `transfers` | No self-transfers |
| Unique `idempotency_key` | `transfers`, `idempotency_records` | No duplicate client requests |
| Unique `(transfer_id, wallet_id, entry_type)` | `ledger_entries` | Exactly one DEBIT and one CREDIT per wallet per transfer |
| Foreign keys | `transfers`, `ledger_entries`, `idempotency_records` | Referential integrity |

**Code reference:** Entity classes with `@Check`, `@UniqueConstraint`, `@ForeignKey`

---

### Q17. What happens when a transfer fails due to insufficient funds?

**Expected answer:**

1. Transfer row is created in `PENDING` state.
2. After locking wallets, balance check fails.
3. Transfer is marked `FAILED` with a `failureReason`.
4. **No** ledger entries are created.
5. **No** wallet balances are changed.
6. Response returned with `status: FAIL`, `responseCode: 5002`.
7. Idempotency record is persisted so duplicate requests replay the same failure.

**Code reference:** `TransferExecutor.failTransfer()`, `TransferIntegrationTest.shouldFailTransferWhenInsufficientFundsWithoutChangingBalances()`

---

### Q18. What is the role of `TransferService` vs `TransferExecutor` vs `IdempotencyResponseLookup`?

**Expected answer:**

| Class | Responsibility |
|-------|----------------|
| `TransferService` | Entry point; validates request; hashes payload; orchestrates idempotency lookup vs execution; handles integrity-violation retry loop |
| `IdempotencyResponseLookup` | Read path: find cached idempotency record or existing transfer; poll for `PENDING → terminal`; build response |
| `TransferExecutor` | Write path: lock wallets, create transfer, debit/credit, write ledger, update status, persist idempotency record |

Separation keeps idempotency reads separate from the transactional transfer mutation.

**Code reference:** `TransferService.java`, `TransferExecutor.java`, `IdempotencyResponseLookup.java`

---

### Q19. How is input validation handled?

**Expected answer:**

Two layers:

1. **Bean Validation** (`@Valid` on controller) — `@NotBlank`, `@NotNull`, `@Min(1)` on DTO fields. Failures handled by `GlobalRestExceptionHandler.handleValidationErrors()` → `6017`.
2. **Business validation** in service — e.g. `fromWalletId != toWalletId` in `TransferService.validateTransferRequest()`.

Malformed JSON is caught by `HttpMessageNotReadableException` handler.

**Code reference:** `CreateTransferRequestDTO.java`, `TransferController.java`, `GlobalRestExceptionHandler.java`

---

### Q20. How does transfer history work?

**Expected answer:**

1. Verify wallet exists.
2. Fetch all `ledger_entries` for the wallet, ordered by `createdAt` desc.
3. Extract distinct `transfer_id`s from those entries.
4. Batch-load `Transfer` entities.
5. For each transfer, load all ledger entries and map to `TransferResponseDTO`.

History is derived from ledger participation, not a separate history table.

**Code reference:** `WalletService.getTransferHistory()`

---

### Q21. Why use stored balance instead of deriving balance from the ledger?

**Expected answer (as implemented):**  
The assignment allows either approach. This codebase **updates `wallets.balance` atomically** during transfer inside the same transaction as ledger writes. Benefits:

- O(1) balance reads for the balance API
- Simpler insufficient-funds check under lock

Tradeoff: balance and ledger must stay in sync — enforced by doing both in one transaction. An alternative design would sum ledger entries (slower reads, no drift if ledger is source of truth).

**Code reference:** `TransferExecutor.applyTransfer()`, `WalletService.getWalletBalance()`

---

### Q22. What do the integration tests cover?

**Expected answer:**

| Test | Behavior verified |
|------|-------------------|
| `shouldProcessTransferAndKeepLedgerBalanced` | Success path, balanced DEBIT/CREDIT, correct balances |
| `shouldReturnOriginalResultForDuplicateIdempotencyKeyWithoutDoubleDebit` | Idempotent replay (success) |
| `shouldReturnOriginalFailedResultForDuplicateIdempotencyKey` | Idempotent replay (failure) |
| `shouldRejectSameIdempotencyKeyWithDifferentPayload` | Idempotency conflict `5003` |
| `shouldReturnSingleProcessedTransferForConcurrentDuplicateIdempotencyKey` | Concurrent same-key requests |
| `shouldPreventDoubleSpendingUnderConcurrentTransfers` | Concurrent different-key double-spend |
| `shouldFailTransferWhenInsufficientFundsWithoutChangingBalances` | Failed transfer, no side effects |
| `shouldRejectTransferWhenSourceAndDestinationAreSame` | Self-transfer rejection |
| `shouldRejectTransferWhenWalletDoesNotExist` | Missing wallet `5113` |
| `shouldReturnWalletBalanceViaApi` | Balance API |
| `shouldReturnTransferHistoryAfterSuccessfulTransfers` | History API |

Tests assert through **HTTP API and observable balances**, not internal implementation.

**Code reference:** `TransferIntegrationTest.java`

---

### Q23. What is `ServiceResponseSupport` and why does it exist?

**Expected answer:**  
A small wrapper that executes a `Supplier<ResponseDTO>` and catches:
- `ResponseException` → returns structured failure response (business errors)
- Other `RuntimeException` → logs and returns generic `5000` failure

This avoids duplicating try/catch in every service method. Note: `GlobalRestExceptionHandler` also handles `ResponseException` for uncaught cases; services using `ServiceResponseSupport` convert exceptions to responses inline.

**Code reference:** `ServiceResponseSupport.java`, `WalletService.java`, `TransferService.java`

---

### Q24. What indexes exist and why?

**Expected answer:**

- `transfers`: indexes on `from_wallet_id`, `to_wallet_id`, `status` — query/filter transfers by wallet and status.
- `ledger_entries`: indexes on `transfer_id`, `wallet_id` — history and ledger lookups.
- Unique indexes implied by `@UniqueConstraint` on idempotency keys and ledger uniqueness.

**Code reference:** `Transfer.java`, `LedgerEntry.java`

---

## Section 3: Hard

### Q25. Walk through what happens when two identical transfer requests arrive at the same time.

**Expected answer:**

1. Both threads hash the request and find no idempotency record.
2. Both enter `TransferExecutor.executeTransfer()` in separate `REQUIRES_NEW` transactions.
3. Both acquire pessimistic locks on the same wallets (serialized by DB).
4. **First thread** creates `PENDING` transfer, processes, writes ledger, marks `PROCESSED`, saves idempotency record, commits.
5. **Second thread** either:
   - Hits unique constraint on `transfers.idempotency_key` when saving the transfer → `DataIntegrityViolationException` → `TransferService` retries idempotency lookup → returns cached response; or
   - Finds existing transfer/idempotency record on lookup after brief polling.
6. Both clients receive the **same response**; wallet is debited **once**.

Test: `shouldReturnSingleProcessedTransferForConcurrentDuplicateIdempotencyKey`.

**Code reference:** `TransferService.executeTransferWithIntegrityRetry()`, `TransferExecutor.persistIdempotencyRecord()`

---

### Q26. Why does the code poll/sleep when waiting for a `PENDING` transfer?

**Expected answer:**  
When a duplicate arrives while the first request is still processing, the transfer exists in `PENDING` but no idempotency record exists yet. `IdempotencyResponseLookup.awaitTerminalTransfer()` polls up to 20 times with 25ms sleep (~500ms max) until status becomes `PROCESSED` or `FAILED`.

This is a **simple in-process coordination** mechanism. In production, alternatives include:
- DB `SELECT ... FOR UPDATE` on the transfer row
- Advisory locks
- Message queue with single consumer per idempotency key
- Returning `202 Accepted` with a status poll endpoint

**Code reference:** `IdempotencyResponseLookup.awaitTerminalTransfer()`, `TransferService.lookupCachedResponseWithRetry()`

---

### Q27. What are the limitations of the current idempotency design?

**Expected answer (strong candidate should identify several):**

1. **Polling with `Thread.sleep`** — blocks servlet threads; does not scale under high duplicate load.
2. **No TTL on idempotency records** — table grows forever; no cleanup strategy.
3. **In-memory coordination only** — polling works within one JVM; cross-instance duplicates rely on DB constraints + retry, which is correct but latency-unpredictable.
4. **Request hash excludes idempotency key** — intentional (key is the lookup key; hash detects payload mismatch), but candidate should articulate this.
5. **Failed transfer still creates a transfer row** — acceptable for audit, but increases table size.
6. **No explicit idempotency key scope per client/tenant** — global uniqueness may be too strict for multi-tenant systems.
7. **Response lost after commit** — if server crashes after commit but before HTTP response, client retry works (idempotency record or transfer row exists) — candidate should confirm this is handled.

**Code reference:** Full idempotency flow across `TransferService`, `IdempotencyResponseLookup`, `TransferExecutor`

---

### Q28. Is this system "exactly-once"? Explain precisely.

**Expected answer:**  
**Exactly-once at the API/effect level** when clients supply an `idempotencyKey` — duplicate requests do not cause duplicate debits or ledger entries.

It is **not** true exactly-once delivery in the distributed-systems sense:
- Network may deliver duplicates; idempotency handles this.
- At-least-once client retries are expected and safe.
- Internally, multiple transfer *attempts* may be initiated (one wins, others get constraint violations or cached responses).

Precise statement: **"At-least-once processing with idempotent semantics producing at-most-once financial side effects."**

---

### Q29. What happens if the server crashes after debiting wallets but before marking the transfer `PROCESSED`?

**Expected answer:**  
The entire `TransferExecutor.executeTransfer()` is `@Transactional`. If the JVM dies mid-transaction, the **database rolls back** the uncommitted transaction — wallet changes, ledger entries, and transfer status update are all undone.

If crash happens **after commit**, state is durable: transfer is `PROCESSED`, balances updated, ledger entries exist, idempotency record saved. Client retry returns cached result.

Edge case to mention: if crash happens after commit but **before** idempotency record insert (same transaction — so this cannot happen; idempotency record is in same transaction). Good candidate notes they are in the **same transaction**, so atomic.

**Code reference:** `TransferExecutor.executeTransfer()` — single transactional method

---

### Q30. Why is pessimistic locking chosen over optimistic locking alone?

**Expected answer:**

**Pessimistic (`PESSIMISTIC_WRITE`):**
- Blocks concurrent debits on the same wallet until the first transaction completes.
- Predictable for financial debit operations where conflicts are frequent under load.
- Combined with sorted lock order to prevent deadlocks.

**Optimistic (`@Version`):**
- Present as secondary guard — `OptimisticLockException` if version mismatch on save.
- Alone, high contention on hot wallets would cause many retries/failures.

For wallet transfers, **pessimistic is the primary strategy** because read-check-write on balance must be atomic and contention is expected.

**Code reference:** `WalletRepository`, `Wallet.version`

---

### Q31. Could a deadlock still occur? How would you diagnose it?

**Expected answer:**

With sorted wallet locking, **wallet-wallet deadlocks are mitigated**. Remaining risks:

- Deadlock with other resources not ordered (e.g. future tables, advisory locks).
- Lock ordering bugs if code path changes.

Diagnosis:
- PostgreSQL: `pg_locks`, deadlock logs
- Enable Hibernate/SQL logging for `FOR UPDATE`
- Thread dumps showing blocked threads on wallet queries

Fix: ensure all code paths lock wallets in sorted order; keep transactions short.

---

### Q32. The unique constraint on `ledger_entries (transfer_id, wallet_id, entry_type)` — what failure does it prevent?

**Expected answer:**  
Prevents duplicate DEBIT or CREDIT entries for the same wallet on the same transfer — e.g. if a bug or retry attempted to insert ledger rows twice. Ensures **exactly one DEBIT and one CREDIT per wallet per transfer** at the database level.

**Code reference:** `LedgerEntry.java`

---

### Q33. How would you scale this service horizontally?

**Expected answer:**

Current design is **mostly safe for horizontal scaling** because:
- State is in PostgreSQL, not in-memory.
- Pessimistic locks work across instances.
- Unique constraints prevent duplicate side effects.

Challenges to address:
1. **Thread.sleep polling** — replace with non-blocking wait or return `202` + status endpoint.
2. **Connection pool sizing** — more instances = more DB connections.
3. **Hot wallet contention** — serializes on DB row locks; may need sharding or per-wallet queues.
4. **Idempotency record growth** — add TTL/archival.
5. **Read replicas** — balance/history reads can use replicas; transfers must use primary.

---

### Q34. How would you implement idempotency if this were a Kafka/event-driven system instead of REST?

**Expected answer (design discussion):**

- Use `idempotencyKey` as **partition key** or **dedup key** in consumer.
- Store processed keys in DB with unique constraint (same `idempotency_records` pattern).
- Consumer processes event idempotently: check key → skip or replay stored result.
- Use **outbox pattern** for atomic "transfer + event publish".
- Consider Kafka idempotent producer + exactly-once semantics (EOS) for pipeline, but business-level idempotency still needed at consumer.

---

### Q35. What security concerns exist in the current implementation?

**Expected answer:**

1. **No authentication/authorization** — any caller can transfer between any wallets.
2. **No rate limiting** — vulnerable to abuse.
3. **Idempotency keys are client-supplied globally unique** — predictable keys could be probed.
4. **LogSanitizer** only strips `\r\n\t` — limited injection protection in logs.
5. **Amount is `Long`** — no currency type, no multi-currency support.
6. **No audit trail of who initiated transfer** — only wallet IDs.

Production would need auth, tenant isolation, signed requests, and audit logging.

**Code reference:** `LogSanitizer.java`, controllers (no security annotations)

---

### Q36. Explain the `ResponseException` vs `GlobalRestExceptionHandler` vs `ServiceResponseSupport` interaction.

**Expected answer:**

- `ResponseException` — business exception carrying `responseCode` and optional `data`.
- `ServiceResponseSupport` — catches `ResponseException` inside service methods and returns `ResponseDTO` (does not propagate to controller advice).
- `GlobalRestExceptionHandler` — catches uncaught `ResponseException`, validation errors, and generic `Exception`.

`TransferService` uses `ServiceResponseSupport`; thrown `ResponseException` from `IdempotencyResponseLookup` during `processTransfer` may be caught by `ServiceResponseSupport` when wrapped in `execute()`. Uncaught exceptions from controller layer hit the advice.

Candidate should note **two parallel error paths** and that HTTP status is always 200 with failure encoded in body (`status: FAIL`) — a deliberate API contract choice.

**Code reference:** `ResponseException.java`, `ServiceResponseSupport.java`, `GlobalRestExceptionHandler.java`

---

### Q37. Why is `open-in-view: false` configured?

**Expected answer:**  
Disables OSIV (Open Session In View). Without it, Hibernate session stays open for the entire HTTP request, lazy associations load during JSON serialization, hiding N+1 problems and blurring transaction boundaries.

With `false`, lazy loading must happen inside `@Transactional` service methods — forces explicit fetch boundaries and is considered best practice for REST APIs.

**Code reference:** `application.yml`

---

### Q38. If you had to choose between deriving balance from ledger vs stored balance, when would you switch?

**Expected answer:**

**Keep stored balance (current)** when:
- Fast balance reads are critical
- Transfer volume is high
- Strong transactional updates are acceptable

**Switch to ledger-derived balance** when:
- Audit/reconciliation requires ledger as single source of truth
- Regulatory requirements demand reconstructability
- Willing to accept slower reads or materialized views

Hybrid: stored balance + periodic reconciliation job comparing `wallet.balance` vs `SUM(ledger)`.

---

### Q39. Design a reconciliation job for this system. What invariants would you check?

**Expected answer:**

Invariants to verify:

1. For every `PROCESSED` transfer: exactly 2 ledger entries, DEBIT amount = CREDIT amount = transfer amount.
2. `wallet.balance` = initial balance + credits − debits (from ledger).
3. No `FAILED` transfer has ledger entries.
4. No negative balances.
5. Sum of all wallet balances is conserved (closed system) unless external deposit/withdraw exists (not in scope).
6. Every `idempotency_record` references a valid `transfer_id`.

On mismatch: alert, quarantine wallet, manual investigation.

---

### Q40. How would you improve the concurrent duplicate-idempotency-key test to be more deterministic?

**Expected answer:**

Current test uses `CountDownLatch` to start threads simultaneously — good but JVM/thread scheduling can be flaky.

Improvements:
- Use a barrier with more threads (e.g. 10 duplicates)
- Inject a controllable delay in `TransferExecutor` (test-only `@Profile`)
- Use Testcontainers with PostgreSQL for production-like locking
- Assert DB state: exactly 1 transfer row, 2 ledger entries, 1 idempotency record for the key
- Use Awaitility instead of fixed sleep loops

**Code reference:** `TransferIntegrationTest.shouldReturnSingleProcessedTransferForConcurrentDuplicateIdempotencyKey()`

---

### Q41. What is the purpose of `RequestHashUtility` using SHA-256 instead of storing raw request JSON?

**Expected answer:**

- **Fixed length** (64 hex chars) regardless of payload size.
- **Normalized comparison** — canonical string `from|to|amount` avoids JSON field-order differences.
- **Privacy** — hash does not expose full request in idempotency table (though payload is also stored separately in `response_payload`).

Candidate may note: hash collision risk with SHA-256 is negligible for this use case.

**Code reference:** `RequestHashUtility.hashTransferRequest()`

---

### Q42. The `persistIdempotencyRecord` swallows `DataIntegrityViolationException` — is that safe?

**Expected answer:**  
Yes, in context. If two concurrent requests both try to insert an idempotency record for the same key, **one wins, one fails on unique constraint**. The loser silently ignores the exception because:

- The winning request already persisted the correct response.
- The caller (or retry loop in `TransferService`) will look up the cached response on subsequent read.

Risk if swallowing hides **other** integrity violations (e.g. bad FK) — mitigated because insert payload is constructed from the same successful transfer in that transaction. A strong candidate suggests logging at debug/warn level for observability.

**Code reference:** `TransferExecutor.persistIdempotencyRecord()`

---

### Q43. How does Hibernate `@Check` differ from JPA validation?

**Expected answer:**

- `@Check` (Hibernate) — generates **database CHECK constraints** enforced by DB on INSERT/UPDATE regardless of how data is written (app, SQL, migration).
- Bean Validation (`@Min`, etc.) — enforced in application layer before persistence.

DB constraints are the **last line of defense**; app validation gives better error messages to clients.

**Code reference:** `Wallet.java`, `Transfer.java`, `CreateTransferRequestDTO.java`

---

### Q44. Propose an API change to support async transfers for long-running operations.

**Expected answer:**

```
POST /v1/transfers → 202 Accepted
{ "transferId": "...", "status": "PENDING", "statusUrl": "/v1/transfers/{id}" }

GET /v1/transfers/{id} → current status + result when terminal
```

Idempotency:
- Same `Idempotency-Key` header returns same `transferId` and status.
- Processing moves to background worker; client polls or receives webhook.

Challenges: outbox, worker idempotency, status visibility before idempotency record exists (partially solved today by polling `PENDING`).

---

### Q45. Whiteboard: Two transfers — `wallet_1 → wallet_2` (100) and `wallet_2 → wallet_1` (50) concurrently. Trace lock order and final balances.

**Expected answer (assuming wallet_1=1000, wallet_2=500):**

Both transfers sort wallets to `[wallet_1, wallet_2]` and lock in that order.

Execution is serialized by DB locks:
1. Suppose T1 (`w1→w2`, 100) runs first: w1=900, w2=600.
2. T2 (`w2→w1`, 50) runs: w2=550, w1=950.

Final: wallet_1=950, wallet_2=550. Total conserved: 1500.

No deadlock because both lock wallet_1 before wallet_2.

---

## Section 4: Follow-up / Discussion Prompts

Use these for open-ended conversation after the structured questions:

1. **What would you change if you had another 2 hours?**
2. **Which part of the PR are you least confident about and why?**
3. **How did you use AI tools in this assignment, and what did you verify manually?**
4. **If a production incident reported duplicate debits, where would you look first?**
5. **How would you explain this system to a non-technical stakeholder?**

---

## Section 5: Quick Reference — Key Files

| Topic | File(s) |
|-------|---------|
| Transfer API | `controller/TransferController.java` |
| Transfer orchestration | `service/transferservice/TransferService.java` |
| Transfer execution | `service/transferservice/TransferExecutor.java` |
| Idempotency lookup | `service/transferservice/IdempotencyResponseLookup.java` |
| Wallet APIs | `controller/WalletController.java`, `service/walletservice/WalletService.java` |
| Pessimistic locking | `repo/WalletRepository.java` |
| Entities | `entity/Wallet.java`, `Transfer.java`, `LedgerEntry.java`, `IdempotencyRecord.java` |
| Integration tests | `test/.../TransferIntegrationTest.java` |
| Exception handling | `configuration/GlobalRestExceptionHandler.java` |
| Request hashing | `utility/RequestHashUtility.java` |

---

## Section 6: Scoring Rubric (for interviewer)

| Level | Typical performance |
|-------|---------------------|
| **Junior / Easy only** | Can explain APIs, tables, and run the app; vague on concurrency/idempotency |
| **Mid / Easy + Medium** | Explains idempotency flow, locking, constraints, and test coverage |
| **Senior / Medium + Hard** | Discusses failure modes, scaling, tradeoffs, reconciliation, and production gaps |
| **Staff+** | Proposes concrete improvements, compares event-driven alternatives, designs ops/reconciliation strategy |

---

*Generated from Wallet Transfer Service codebase analysis. Aligns with `ASSIGNMENT.md`, `README.md`, and `evaluation_guide.md`.*
