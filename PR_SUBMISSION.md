# Pull Request — Wallet Transfer Service (Sanchay Mehrotra)

> Copy this content into your GitHub PR description when opening the PR from `solution/sanchay-mehrotra` → `main`.

## Summary

Implemented a **Wallet Transfer Service** (Spring Boot 3.3.7 / Java 21) supporting idempotent wallet-to-wallet transfers with double-entry ledger recording, pessimistic locking for concurrency safety, and clean layered architecture (controller → service → repository → entity).

**APIs delivered:**
- `POST /v1/transfers` — idempotent transfer with `PENDING → PROCESSED | FAILED` state machine
- `POST /v1/wallet-balance` — read stored wallet balance
- `POST /v1/transfer-history` — transfer history for a wallet

**Also included:** Swagger annotations, Postman collection with test scripts, behavioral integration tests (including concurrency), JaCoCo/SonarQube config, and Maven CI workflow.

## AI disclosure

### 1. Tool used
**Cursor** (AI-assisted IDE with agent/chat capabilities).

### 2. How I generally use the tool
I use Cursor as a development accelerator, not a substitute for engineering judgment:
- **Scaffolding & exploration** — bootstrap project structure, read reference codebases, and align with existing patterns (e.g. MedConnect `ResponseException` flow).
- **Iterative refinement** — review AI-generated code, request targeted fixes (Sonar issues, test gaps, idempotency edge cases), and validate with `mvn test`.
- **Documentation & tooling** — generate Postman collections, README sections, and PR notes; I edit and verify before committing.
- **I retain ownership** of architecture decisions (schema design, locking strategy, idempotency model, transaction boundaries) and can explain every part of the solution in review.

### 3. Session transcript / prompts

Full Cursor agent transcript is available at:
`docs/ai-session-transcript.jsonl` (copy of the session transcript in this repo).

**All prompts used in this session:**

1. Read all the md files in project and create the project APIs accordingly. Structure should be similar to MedConnect-User-Engagement. Create DTOs locally (no service-dto dependency).
2. Use ResponseException pattern — throw and catch in each API like MedConnect.
3. Use indexes and constraints directly on entities.
4. Analyse this project for SonarQube and fix all vulnerabilities.
5. Fix SonarQube warning on entity classes (Timestamp → Instant).
6. Fix specific Sonar issues: deprecated defaultString, LOG_FORMAT constant, ResponseException serializable data, TransferIntegrationTest fixes, TransferService variable rename.
7. Add Swagger annotations on controllers and create Postman collection with all test scenarios.
8. Confirm behavioral testing and concurrency safety coverage.
9. Fix additional Sonar issues (ApiResponses wrapper, etc.).
10. Is the Postman collection updated with all changes?
11. Check copilot-instructions.md and ensure the codebase passes all review criteria.
12. Is the Postman collection updated?
13. Help with git push 403 permission error.
14. Create PR description per pull request template.

## Schema Design

Four tables, defined via JPA entities with indexes/constraints (Hibernate `ddl-auto: update`; PostgreSQL supported via `application-postgres.yml`):

### `wallets`
| Column | Notes |
|--------|-------|
| `wallet_id` (PK) | String identifier |
| `balance` | `>= 0` (DB check constraint) |
| `version` | Optimistic lock (`@Version`) |
| `created_at`, `updated_at` | `Instant` timestamps |

### `transfers`
| Column | Notes |
|--------|-------|
| `id` (PK) | UUID |
| `idempotency_key` | **Unique** (`uq_transfers_idempotency_key`) |
| `from_wallet_id`, `to_wallet_id` | FK to wallets; check `from <> to` |
| `amount` | Check `> 0` |
| `status` | `PENDING`, `PROCESSED`, `FAILED` |
| `failure_reason` | Populated on `FAILED` |

**Indexes:** `from_wallet_id`, `to_wallet_id`, `status`

### `ledger_entries`
| Column | Notes |
|--------|-------|
| `id` (PK) | Auto-increment |
| `wallet_id`, `transfer_id` | FKs |
| `entry_type` | `DEBIT` or `CREDIT` |
| `amount` | Check `> 0` |

**Unique:** `(transfer_id, wallet_id, entry_type)` — prevents duplicate ledger rows  
**Indexes:** `transfer_id`, `wallet_id`

### `idempotency_records`
| Column | Notes |
|--------|-------|
| `id` (PK) | Auto-increment |
| `idempotency_key` | **Unique** (`uq_idempotency_key`) |
| `transfer_id` | FK to transfers |
| `request_hash` | SHA-256 of canonical request payload |
| `response_status`, `response_code`, `response_message`, `response_payload` | Cached API response |

## Idempotency Strategy

**Goal:** exactly-once API semantics when `idempotencyKey` is provided.

1. **Request hashing** — `RequestHashUtility` hashes `fromWalletId`, `toWalletId`, and `amount`.
2. **Fast path** — `IdempotencyResponseLookup` (read-only, `REQUIRES_NEW` transaction) checks `idempotency_records` by key:
   - Same hash → return cached response (success or failure).
   - Different hash → `5003` idempotency conflict.
3. **Fallback** — if no idempotency record, lookup `transfers` by key; if found in `PENDING`, wait briefly for terminal state before replaying.
4. **New transfer** — `TransferExecutor` runs in `REQUIRES_NEW` transaction:
   - Creates `PENDING` transfer (unique key enforced by DB).
   - On success: debit/credit wallets, write 2 ledger entries, `markProcessed()`, persist idempotency record.
   - On insufficient funds: `markFailed()`, persist idempotency record (no ledger entries).
5. **Concurrent duplicates** — unique constraints on `transfers.idempotency_key` and `idempotency_records.idempotency_key` cause `DataIntegrityViolationException`; loser retries lookup in a fresh transaction and returns the winner's response.

**Retry-safe:** failed transfers and successes are both cached; duplicate requests never double-debit.

## Concurrency Strategy

**Problem:** two transfers debiting the same wallet simultaneously must not double-spend.

**Approach:**
1. **Pessimistic row locks** — `WalletRepository.findByWalletIdsForUpdate()` with `PESSIMISTIC_WRITE`.
2. **Deadlock prevention** — wallets locked in **sorted wallet ID order**.
3. **Atomic transaction** — balance update + ledger writes + status change in one `REQUIRES_NEW` transaction (`TransferExecutor`).
4. **DB-level guards** — `balance >= 0` check constraint, optimistic `@Version` on wallets.
5. **Separate idempotency transactions** — lookup/execution split so integrity violations don't poison the outer request transaction.

**Verified by tests:** `shouldPreventDoubleSpendingUnderConcurrentTransfers` and `shouldReturnSingleProcessedTransferForConcurrentDuplicateIdempotencyKey`.

## How to Run

```bash
# Prerequisites: Java 21, Maven 3.9+

# Clone and checkout solution branch
git checkout solution/sanchay-mehrotra

# Run locally (H2 in-memory, seeded via data.sql)
mvn spring-boot:run

# Optional: PostgreSQL
mvn spring-boot:run -Dspring-boot.run.profiles=postgres
# Set DB_HOST, DB_PORT, DB_NAME, DB_USERNAME, DB_PASSWORD
```

**Endpoints:**
- Base URL: `http://localhost:8080/api/wallet-transfer`
- Swagger UI: `http://localhost:8080/api/wallet-transfer/swagger-ui.html`
- Postman: import `postman/Wallet-Transfer-Service.postman_collection.json`

## How to Test

```bash
# All unit + integration tests (16 tests)
mvn test

# Full verify (includes JaCoCo coverage report)
mvn verify
```

**Integration test coverage (`TransferIntegrationTest`):**
- Successful transfer + balanced DEBIT/CREDIT ledger
- Idempotent replay (success and failure paths)
- Idempotency key conflict (different payload)
- Insufficient funds (no balance change, no ledger)
- Same source/destination wallet validation
- Wallet not found
- Wallet balance API
- Transfer history API
- Concurrent double-spend prevention
- Concurrent duplicate idempotency key

**Manual testing:** run Postman collection folders (Transfers, Wallet, Transfer History, End-to-End Flow) with Collection Runner.

## Tradeoffs / Assumptions

- **Stored balance vs ledger-derived balance** — chose stored balance updated atomically during transfer for simpler reads; ledger is audit trail. Both stay consistent within transactions.
- **H2 for local/test, PostgreSQL for production** — schema managed via JPA entities (`ddl-auto: update`), not Flyway migrations.
- **Idempotency in-flight wait** — brief polling when duplicate hits a `PENDING` transfer; production might use advisory locks or a `PROCESSING` idempotency state.
- **No authentication** — out of scope for assignment; APIs are open on localhost.
- **Response envelope** — follows MedConnect-style `ResponseDTO` with numeric `responseCode` (20001 success, 5xxx/6xxx errors).
- **AI-assisted development** — used Cursor for scaffolding and iteration; all design decisions and final code reviewed manually.

## Checklist

- [x] Tests pass (`mvn test` — 16/16)
- [x] Lint passes (no dedicated Java linter in CI; SonarQube config present)
- [x] Format check passes (standard Java/Maven project)
- [x] README or notes updated (`README.md`, `PR_SUBMISSION.md`, Postman collection)
- [x] PR description explains schema, idempotency, and concurrency
