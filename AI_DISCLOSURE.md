# AI Usage Disclosure

This document outlines how Artificial Intelligence was utilized during the development, testing, and refinement of the **Wallet Transfer Service**, in compliance with the assignment instructions.

---

## 1. Tool Used

- **Primary Tool**: **Google Antigravity** (Advanced Agentic AI Pair-Programming System).
- **Underlying Model**: Advanced Agentic Reasoning Model.
- **Environment**: Windows 11, Go 1.23+, PostgreSQL 16 (`jackc/pgx/v5`).

---

## 2. How the Tool Was Used

Antigravity was employed as an active **pair programmer and systems design sounding board**, rather than an autonomous code generator. The engineering workflow followed a structured, iterative methodology:

### A. Pre-Implementation Problem Formulation & Architectural Design
- Prior to writing code, Antigravity was prompted to analyze `assignment.md` and produce a comprehensive technical specification (`problem_understanding.md`).
- We debated architectural tradeoffs:
  - **Ledger Model**: Choosing stored wallet balances with strict transactional double-entry ledger logging (`CREDIT` and `DEBIT` pairs) for $O(1)$ balance lookups alongside an atomic audit trail.
  - **Concurrency & Deadlock Prevention**: Determining deterministic lock acquisition (`wallet_a < wallet_b`) to prevent cyclic waits during bidirectional concurrent transfers.
  - **Idempotency Lifecycle**: Formulating a two-phase protocol (short committed reservation with stale-owner recovery) to avoid holding transaction locks across client retries.

### B. Human-in-the-Loop Steering on Infrastructure & Persistence
- When selecting the database setup, the user explicitly decided to use a native, persistent PostgreSQL 16 service on Windows rather than an ephemeral container, ensuring local data persistence and realistic database connection pool behavior.
- In CI, containerized PostgreSQL 16 was configured via Docker Compose to guarantee mandatory, automated testing without silent test skips.

### C. Systematic PR Review Triage & Invariant Hardening
- Following a peer review of PR #169 containing 17 technical comments, Antigravity was used to methodically triage, diagnose, and remediate every reported issue:
  - Eliminating preimage delimiter collisions via length-delimited hashing (`len:val:len:val:amount`).
  - Preventing information leakage by sanitizing generic HTTP 500 error messages while logging details server-side.
  - Preserving original HTTP status codes (201 Created vs 422 Unprocessable Entity) on idempotency replays.
  - Distinguishing 404 Not Found vs 409 Conflict vs 500 Internal Error across handlers.
  - Protecting against arithmetic overflow via `CanCredit` checking `math.MaxInt64`.
  - Ensuring single-source schema migrations using Go 1.16+ `//go:embed`.
  - Eliminating false reconciliation mismatches under live concurrency via `SELECT ... FOR UPDATE` row locks.

### D. Test-Driven Concurrency & Stress Verification
- Antigravity was used to construct rigorous concurrency regression tests:
  - 20 concurrent bidirectional transfers in each direction (40 total) between two wallets to verify zero deadlocks.
  - 10 concurrent debits against a shared wallet to verify zero double-spending (matching `TestTransferService_ConcurrentDebits_NoDoubleSpend`).
  - Concurrent background transfers executed during active balance reconciliation to verify zero false mismatches.
- All tests were executed against a real PostgreSQL 16 instance with bounded contexts to eliminate hanging test suites.

---

## 3. Session Transcript & Prompt Records

The complete transcript of all 70 interaction turns—including exact prompt text, tool invocations, and AI responses—is preserved in this repository:
- **Readable Session Transcript**: [`AI_TRANSCRIPT.md`](./AI_TRANSCRIPT.md)
- **Raw Agent Interaction Log**: Persisted in the session metadata logs.

---

## 4. Chronological List of All Prompts

Below is the complete chronological log of all 70 explicit prompts provided during the development session:

| # | Timestamp (UTC) | Phase | User Prompt |
|---|---|---|---|
| **1** | `2026-09-10 17:20:44` | Architecture | *from assignment.md file outline your understanding and approach before implementing the full solution in problem_understanding.md file* |
| **2** | `2026-09-10 18:46:53` | Architecture | *i will use postgresql* |
| **3** | `2026-09-10 18:50:16` | Architecture | *can you explain the changes first* |
| **4** | `2026-09-10 18:50:53` | Architecture | *\| \*\*Database Engine\*\* \| PostgreSQL (with `jackc/pgx/v5`) \| Industry-standard transactional engine supporting true row-level locks (`SELECT FOR UPDATE`), strict constraints, and connection pooling. Docker Compose provided for 1-command startup. explain this* |
| **5** | `2026-09-10 18:52:53` | Infrastructure | *i don't want to use docker for database because data will be not persistent* |
| **6** | `2026-09-10 18:57:05` | Infrastructure | *how to check its installed or not* |
| **7** | `2026-09-10 18:58:33` | Infrastructure | *2nd option* |
| **8** | `2026-09-10 19:00:45` | Git & Commit | *give me an appropriate commit message for this* |
| **9** | `2026-09-10 19:05:36` | Scaffolding | *now set up the project as given in assignment.md* |
| **10** | `2026-09-10 20:28:47` | Review Fixes | *problem_understanding.md:176 Using a column named key in the proposed idempotency_records schema is inconsistent with the rest of the document (which uses idempotency_key) and tends to require awkward quoting in SQL; naming it idempotency_key improves clarity and consistency.* |
| **11** | `2026-09-10 20:43:46` | Review Fixes | *The documentation includes a few concrete technical inaccuracies/inconsistencies (PostgreSQL transaction wording, idempotency hash definition, schema naming, and updated_at semantics) that should be corrected to avoid guiding the implementation incorrectly.* |
| **12** | `2026-09-11 17:40:46` | Review Fixes | *PostgreSQL doesn't have an "immediate" transaction mode; describing a "serializable/immediate transaction" is inaccurate and could mislead the implementation approach.* |
| **13** | `2026-09-11 18:19:10` | Review Fixes | *PostgreSQL doesn't have an "immediate" transaction mode; describing a "serializable/immediate transaction" is inaccurate and could mislead the implementation approach.* |
| **14** | `2026-09-11 18:35:54` | Review Fixes | *internal/domain/transfer.go:42 The domain validation reports a missing idempotency key as ErrInvalidAmount, whose message says the amount is invalid. Any caller using Transfer.Validate would return the wrong failure reason for a missing key; add/use a dedicated invalid-idempotency-key error instead.* |
| **15** | `2026-09-11 18:42:35` | Review Fixes | *internal/handler/wallet_handler.go:51 This 500 response forwards the repository error text directly to clients, potentially exposing database details. Keep the error in server-side logs and return a generic internal-error response.* |
| **16** | `2026-09-11 18:44:08` | Review Fixes | *internal/handler/wallet_handler.go:72 & :93 This 500 response forwards the repository error text directly to clients, potentially exposing database details. Keep the error in server-side logs and return a generic internal-error response.* |
| **17** | `2026-09-11 18:46:12` | Review Fixes | *internal/repository/postgres/idempotency_repo.go:33 The idempotency reservation is inserted in the same transaction as the entire transfer, so an overlapping request cannot see IN_PROGRESS: it waits on the unique-index conflict until the first transaction commits, then replays... use a short committed reservation/lock protocol with explicit stale-owner recovery if immediate rejection is required.* |
| **18** | `2026-09-11 18:52:03` | Review Fixes | *internal/service/transfer_service.go:45 The idempotency record is consulted only after validating the full request. Reusing an existing key with a changed but invalid amount (or missing wallet ID) returns 400/404 instead of the documented 409 payload-conflict response... Resolve an existing key against a canonical request fingerprint before business-field validation...* |
| **19** | `2026-09-11 18:55:03` | Review Fixes | *internal/service/transfer_service.go:82 The documented IN_PROGRESS/409 path is not observable here: the idempotency row is inserted and finalized inside the same transaction... either separate/commit the reservation with crash recovery or document blocking/replay semantics.* |
| **20** | `2026-09-11 18:57:09` | Review Fixes | *internal/service/transfer_service.go:36 These validation errors map to incorrect HTTP results: an empty idempotency key is an untyped error that the handler returns as 500, while missing wallet IDs are returned as ErrWalletNotFound and become 404... use dedicated validation errors and map them accordingly.* |
| **21** | `2026-09-11 19:00:00` | Review Fixes | *internal/service/transfer_service_test.go:329 This concurrency test also waits on all database calls with context.Background() and no deadline. A regression that blocks a wallet row lock will make the test process hang indefinitely rather than report a failed double-spend test; bound the request context and wait with a timeout.* |
| **22** | `2026-09-11 19:02:18` | Review Fixes | *migrations/000001_init_schema.sql:1 postgres.Migrate embeds and executes internal/repository/postgres/schema.sql; this migration file is never applied by the application... Use one migration source or make startup execute this migration set.* |
| **23** | `2026-09-11 19:06:17` | Review Fixes | *In [cmd/api/main.go](https://github.com/Robustrade/wallet-transfer-assignment/pull/169#discussion_r3992031441): databaseUrl logged containing credentials in plaintext.* |
| **24** | `2026-09-11 19:08:33` | Review Fixes | *In [internal/domain/wallet.go](https://github.com/Robustrade/wallet-transfer-assignment/pull/169#discussion_r3992031464): Credit arithmetic can overflow MaxInt64 without boundary check.* |
| **25** | `2026-09-11 19:11:43` | Review Fixes | *In [internal/repository/postgres/schema.sql](https://github.com/Robustrade/wallet-transfer-assignment/pull/169#discussion_r3992031495): Seeded system treasury lacks corresponding double-entry opening ledger record.* |
| **26** | `2026-09-11 19:18:42` | Review Fixes | *In [internal/service/transfer_service.go](https://github.com/Robustrade/wallet-transfer-assignment/pull/169#discussion_r3992031518): UpdateTransferStatus error ignored on failure branch.* |
| **27** | `2026-09-11 19:22:37` | Review Fixes | *In [internal/service/transfer_service.go](https://github.com/Robustrade/wallet-transfer-assignment/pull/169#discussion_r3992031535): UpdateIdempotency error unhandled during completion path.* |
| **28** | `2026-09-11 19:27:21` | Review Fixes | *In [internal/service/transfer_service.go](https://github.com/Robustrade/wallet-transfer-assignment/pull/169#discussion_r3992031551): Currency compatibility between wallets is unvalidated.* |
| **29** | `2026-09-11 19:33:22` | Review Fixes | *In [internal/service/wallet_service.go](https://github.com/Robustrade/wallet-transfer-assignment/pull/169#discussion_r3992031562): Wallet creation and initial funding transfer are split across separate transactions.* |
| **30** | `2026-09-11 19:37:17` | Review Fixes | *In [internal/service/wallet_service.go](https://github.com/Robustrade/wallet-transfer-assignment/pull/169#discussion_r3992031572): system_treasury stored balance is not debited during funded wallet creation.* |
| **31** | `2026-09-11 19:39:11` | Review Fixes | *In [internal/domain/idempotency.go](https://github.com/Robustrade/wallet-transfer-assignment/pull/169#discussion_r3992031576): Preimage hash format ambiguous when wallet IDs contain ":" delimiter.* |
| **32** | `2026-09-11 19:42:33` | Review Fixes | *In [internal/handler/transfer_handler.go](https://github.com/Robustrade/wallet-transfer-assignment/pull/169#discussion_r3992031619): Fallback error exposes raw database details to clients.* |
| **33** | `2026-09-11 19:45:36` | Review Fixes | *In [internal/handler/transfer_handler.go](https://github.com/Robustrade/wallet-transfer-assignment/pull/169#discussion_r3992031651): Replayed idempotent requests force 200 OK instead of preserving original HTTP status code.* |
| **34** | `2026-09-11 19:49:16` | Review Fixes | *In [internal/handler/transfer_handler.go](https://github.com/Robustrade/wallet-transfer-assignment/pull/169#discussion_r3992031678): GetTransfer maps all database errors to 404 Not Found.* |
| **35** | `2026-09-11 19:51:37` | Review Fixes | *In [internal/handler/wallet_handler.go](https://github.com/Robustrade/wallet-transfer-assignment/pull/169#discussion_r3992031706): CreateWallet maps unique-constraint failures and DB errors to 400 Bad Request.* |
| **36** | `2026-09-11 19:55:06` | Review Fixes | *In [internal/service/wallet_service.go](https://github.com/Robustrade/wallet-transfer-assignment/pull/169#discussion_r3992031730): Balance reconciliation reads wallet and ledger in separate uncommitted queries, causing false mismatches.* |
| **37** | `2026-09-11 19:57:45` | Review Fixes | *In [pkg/testutil/db.go](https://github.com/Robustrade/wallet-transfer-assignment/pull/169#discussion_r3992031758): Test helper skips integration tests when PostgreSQL is missing.* |
| **38** | `2026-09-11 19:59:45` | Review Fixes | *In [internal/repository/postgres/db.go](https://github.com/Robustrade/wallet-transfer-assignment/pull/169#discussion_r3992031795): Dual schema sources (embedded schema.sql vs migrations directory).* |
| **39** | `2026-09-11 20:01:21` | Review Fixes | *In [internal/service/transfer_service_test.go](https://github.com/Robustrade/wallet-transfer-assignment/pull/169#discussion_r3992031836): Deadlock regression test waits indefinitely without timeout.* |
| **40** | `2026-09-11 21:09:56` | Verification | *have we coveed everthing?* |
| **41** | `2026-09-11 21:14:11` | Verification | *before there were so many errors so now is it all ok ?* |
| **42** | `2026-09-11 21:15:55` | AI Disclosure | *AI disclosure: Detail how you used AI to help with your submission...* |
| **43** | `2026-09-12 02:52:54` | Git & Commit | *give me proper commit message* |
| **44** | `2026-09-12 02:54:56` | Git & Commit | *sure* |
| **45** | `2026-09-12 03:16:08` | Review Fixes | *again so many comments* |
| **46** | `2026-09-12 03:16:28` | Review Fixes | *[Follow-up Review Feedback on PR #169 covering CI fallback, owner-token lease fencing, key length validation, barrier race test, funding key namespace, test advisory lock timing, and documentation alignment]* |
| **47** | `2026-09-11 21:55:00` | Review Fixes | *verification and test validation* |
| **48** | `2026-09-11 21:56:04` | Git & Commit | *commit* |
| **49** | `2026-09-11 22:09:51` | Review Fixes | *[Follow-up Review Feedback on PR #169 covering lease heartbeats, database-level ledger constraints, migration version tracking, isolated test databases, terminal failure persistence, and test timeouts]* |
| **50** | `2026-09-11 22:10:42` | Review Fixes | *execution, verification, and disclosure update* |
| **51** | `2026-09-11 22:30:15` | Git & Commit | *push* |
| **52** | `2026-09-11 22:35:10` | Review Fixes | *[Follow-up Review Feedback on PR #169 covering atomic multi-row ledger inserts, in-flight heartbeat lease cancellation, strict migration version lookup error handling, migration runner advisory locks, transfers table constraint triggers, and ledger immutability triggers]* |
| **53** | `2026-09-11 22:41:41` | Git & Commit | *push the changes* |
| **54** | `2026-09-12 06:42:19` | Review Fixes | *internal/handler/wallet_handler.go:35 When a requested initial balance exceeds the system treasury, WalletService.CreateWallet returns ErrInsufficientFunds, but this switch falls through to the generic 500 response. That is a normal business rejection rather than an internal failure; map it to a client/business status (for example 422, as the transfer handler does) so callers receive a retryable, actionable result.* |
| **55** | `2026-09-12 06:46:27` | Review Fixes | *[Follow-up Review Feedback on PR #169 covering deterministic ledger ordering tie-breaker, heartbeat join before commit decision, txCtx propagation to transaction callbacks, atomic preflight idempotency rechecks, and commit-ambiguity preservation]* |
| **56** | `2026-09-12 06:54:34` | Review Fixes | *In [internal/service/transfer_service.go]: All heartbeat errors other than ErrIdempotencyLeaseLost are silently ignored... Track the heartbeat failure and cancel or otherwise fail the lease when refreshes cannot be completed reliably. resolve this also then push* |
| **57** | `2026-09-12 07:39:02` | Review Fixes | *internal/handler/wallet_handler.go:35 When a requested initial balance exceeds the system treasury, WalletService.CreateWallet returns ErrInsufficientFunds, but this switch falls through to the generic 500 response... dont push anything* |
| **58** | `2026-09-12 07:45:09` | Review Fixes | *In [internal/config/config.go]: When DATABASE_URL is omitted, the service silently runs with a committed, known PostgreSQL superuser credential... Require an explicitly configured URL in non-development environments and use a least-privileged application role instead of a runtime default superuser connection.* |
| **59** | `2026-09-12 07:54:08` | Review Fixes | *In [internal/repository/postgres/idempotency_repo.go]: This stale-reclaim path does not verify that the existing reservation has the same request hash... Require existing.RequestHash == record.RequestHash before reclaiming (or include it in the UPDATE predicate).* |
| **60** | `2026-09-12 07:58:30` | Review Fixes | *In [internal/service/transfer_service.go]: If this fenced finalization fails, this branch still marks the operation as successful and returns the processed response... propagate the update error and keep the reservation for recovery instead of returning success* |
| **61** | `2026-09-12 08:02:27` | Review Fixes | *In [internal/service/transfer_service.go]: GetTransferByID returns a nil transfer for operational errors as well as not-found errors, so transfer == nil treats an unavailable database/read during commit-ambiguity reconciliation as proof that the transaction rolled back... Only delete after an explicit ErrTransferNotFound/pgx.ErrNoRows; retain the reservation for all other errors.* |
| **62** | `2026-09-12 08:15:27` | Review Fixes | *In [internal/service/wallet_service.go]: CreateWallet is wired to the public POST /wallets route, but this branch treats the caller's InitialBalance as a treasury-funded deposit. An unauthenticated caller can create a wallet and then use POST /transfers with the known system_treasury account, effectively minting or draining funds. Keep treasury funding behind an authorized/admin path or seed balances out of band; do not expose this workflow on the public router.* |
| **63** | `2026-09-12 08:33:43` | Review Fixes | *In [migrations/migrations.go]: If the advisory-lock call fails after PostgreSQL has actually acquired the session lock (for example, a lost response or canceled context), the unlock defer has not been registered yet. lockConn.Release() can then return a session holding 888999 to the pool, causing later migration attempts to block indefinitely. Register the unlock defer immediately after acquiring the connection, before attempting pg_advisory_lock, or use a transaction-scoped advisory lock.* |
| **64** | `2026-09-12 08:38:00` | Review Fixes | *In [migrations/000001_init_schema.sql]: These deferred triggers and the immutability trigger are the database's last line of defense for the ledger, but the integration tests only validate pair shape in CreateLedgerEntries before SQL executes and exercise valid application transfers. They do not attempt an invalid wallet/type pair or an UPDATE/DELETE against PostgreSQL, so a broken trigger definition could pass the suite while allowing unbalanced or mutable ledger data. Add integration assertions that malformed pairs fail at commit and ledger mutations are rejected.* |
| **65** | `2026-09-12 08:44:00` | Review Fixes | *In [internal/domain/errors.go]: ErrInvalidIdempotencyKey is described as covering empty and overlong keys, but ExecuteTransfer returns ErrMissingIdempotencyKey for an empty key. This makes the error text misleading wherever the length-specific error is surfaced; make this message describe only the overlong-key condition or align the service behavior.* |
| **66** | `2026-09-12 09:15:00` | Review Fixes | *[Comprehensive Review on PR #169: CanDebit strict positive amount, bounded ledger pagination, safe-by-default APP_ENV & explicit ADMIN_KEY requirement, owner-token lease update fencing, atomic pending-to-terminal transfer status enforcement, commit-time transfer status trigger verification, migration role separation, and post-acquisition advisory lock cancellation test redesign]* |
| **67** | `2026-09-12 10:00:00` | Review Fixes | *[Follow-up Review Feedback on PR #169 covering pool headroom & tx semaphore, terminal-only stale recovery, error propagation during transfer lookup, un-locked preflight removal, PostgreSQL 16 schema permissions, and test database validation]* |
| **68** | `2026-09-12 10:35:00` | Review Fixes | *[Comprehensive Hardening on PR #169: Dedicated heartbeat connection pool isolation, dynamic capacity derivation & immutable tx semaphore, bounded idempotency reservation retry & orphaned state cleanup, single-connection pool (MaxConns=1) migration safety & deferred rollback, terminal transfer status immutability & non-PROCESSED ledger trigger enforcement, CI test execution under least-privileged wallet_app role, test DB allowlist verification & credential sanitization, and full documentation/turn reconciliation]* |
| **69** | `2026-09-12 10:55:00` | Review Fixes | *In [internal/repository/postgres/idempotency_repo.go]: ReserveIdempotency initializes record.UpdatedAt once, and all terminal finalization callers pass that same value back here... Always set updated_at to the current UTC time when updating the record.* |
| **70** | `2026-09-12 11:45:00` | Review Fixes | *[Comprehensive Review Remediation on PR #169: Service-level field bounds validation (wallet ID <= 64, name <= 255, currency == 3) mapped to HTTP 400 Bad Request; removal of unfenced DeleteIdempotency and introduction of compare-and-swap DeleteStaleInProgress(..., maxUpdatedAt); restricting ledger NULL transfer_id strictly to system_treasury opening record; statement-level trigger trg_prevent_ledger_truncate on ledger_entries; complete immutability of terminal transfers on UPDATE; migration runner role separation via MigrationDatabaseURL; test database admin isolation eliminating single-connection self-deadlocks (MaxConns=1); and deterministic post-acquisition advisory lock cancellation test redesign]* |

---

## 5. Candidate Preparedness for PR Discussion

The author is fully prepared to explain and defend every design decision and line of code in the implementation during the technical discussion:

1. **Short-Committed Reservation Protocol & Owner-Token Lease Fencing**:
   - Why we insert `IN_PROGRESS` in an immediate short transaction rather than keeping an uncommitted lock open during the entire transfer: under burst retries, overlapping requests immediately receive `409 Conflict` without tying up database connection pool workers.
   - Stale-owner recovery & fencing: if a worker process crashes while holding an `IN_PROGRESS` reservation, reservations older than 30 seconds are reclaimed safely. To prevent slow/paused workers from corrupting or deleting state after a lease reclaim, every reservation and reclaim generates an `owner_token`. Updates and deferred cleanups are fenced by `owner_token`.
   - Matching Request Hash on Stale Reclaim: stale reservations require `existing.RequestHash == record.RequestHash` before attempting reclaim, and include `request_hash = $5` in the atomic SQL `UPDATE` predicate. This prevents concurrent callers with differing payloads from stealing or overwriting stale reservations; differing payloads immediately receive `ErrIdempotencyConflict` (`409 Conflict`).
   - Lease Heartbeats & Active In-Flight Cancellation: while a transfer is actively running, a background heartbeat periodically refreshes `updated_at` every 5 seconds so live requests are never reclaimed prematurely. If heartbeat encounters `ErrIdempotencyLeaseLost`, it actively cancels the in-flight context (`cancelTx()`), instantly releasing row locks to prevent duplicate work or contention with the replacement owner. All database operations in the transaction callback strictly receive `txCtx` so cancellation takes effect immediately.
   - Unrefreshable Heartbeat Tracking: non-lease-loss heartbeat failures (e.g. database timeouts, dropped connections) are tracked across consecutive failures and elapsed time; if refreshes cannot be completed reliably before the 30-second reclaim timeout, `cancelTx()` aborts the transaction immediately to prevent holding locks with an unrefreshed lease.
   - Heartbeat Stop/Join & Authoritative Commit: the service stops and joins the heartbeat before checking lease status; if the transaction committed, the commit is authoritative, eliminating races where post-commit ticker ticks report false lease loss.
   - Commit-Ambiguity Preservation & Fenced Finalization Error Propagation: if a transaction completed its operations but commit reports an ambiguous failure, the `IN_PROGRESS` reservation is preserved and reconciled rather than blindly deleted. If `UpdateIdempotency` during reconciliation fails, the update error is propagated and `success = true` is disarmed, keeping the reservation intact in the database for recovery. Subsequent retries query `GetTransferByIdempotencyKey`, detect the committed transfer, finalize the idempotency record, and return the cached result without ever executing a second transfer.
   - Strict Rollback Confirmation: during commit-ambiguity reconciliation, in-progress reservations are only deleted when `GetTransferByID` returns an explicit `ErrTransferNotFound` or `pgx.ErrNoRows`. All operational errors (network timeouts, unavailable database) retain the reservation, preventing retries from duplicating debits while the transfer outcome remains unknown.
   - Preflight Idempotency Recheck: if preflight validation fails, the key is rechecked against `idempotency_records` so concurrent key reuses receive `409 Conflict/In-Progress` with the exact same precedence as sequential reuses.
2. **Deterministic Deadlock Prevention**:
   - Why locking ordering is sorted lexicographically (`from_id < to_id ? (from, to) : (to, from)`): breaks the circular wait condition (Coffman condition) across concurrent bidirectional transfers.
   - Initial wallet funding locks `system_treasury` before inserting the user wallet to adhere strictly to the global lock hierarchy.
3. **Double-Entry Ledger Integrity & Database-Level Constraints**:
   - Stored wallet balances are updated atomically alongside two ledger entries (`DEBIT` and `CREDIT`) within a single ACID transaction.
   - Multi-row atomic SQL inserts (`INSERT INTO ledger_entries (...) VALUES (...), (...)`) guarantee statement-level atomicity even when invoked against connection pools without an explicit outer transaction.
   - Database-level composite unique indexes (`(transfer_id, type)` and `(transfer_id, wallet_id)`), together with commit-deferred PostgreSQL constraint triggers (`trg_check_ledger_pair` on `ledger_entries` and `trg_check_transfer_processed` on `transfers`), guarantee that no transfer can commit with status `PROCESSED` without exactly one matching DEBIT on `from_wallet_id` and one CREDIT on `to_wallet_id` equal to the transfer amount.
   - Immutability trigger (`trg_prevent_ledger_mutation`) strictly forbids any `UPDATE` or `DELETE` on `ledger_entries`, preserving the immutable financial audit log.
   - Deterministic Ledger Pagination: `GetLedgerByWalletID` orders by `created_at ASC, id ASC`, using the primary key `id` as a unique tie-breaker to prevent nondeterministic history ordering across identical timestamps.
   - `ReconcileBalance` performs `SELECT ... FOR UPDATE` row locking on the wallet inside a transaction while aggregating ledger records, eliminating false concurrency mismatches.
4. **Length-Delimited Preimage Hashing**:
   - Why `%d:%s:%d:%s:%d` prevents delimiter injection attacks across colon characters.
5. **HTTP Status Code Fidelity on Replays**:
   - Caching `response_code` in `idempotency_records` so that a replayed successful transfer returns `201 Created` while a replayed overdraft returns `422 Unprocessable Entity`.
6. **Applied Migration Version Tracking & Advisory Locking**:
   - Schema migrations are recorded in a `schema_migrations (version, applied_at)` table inside an atomic transaction, protected by a dedicated PostgreSQL advisory lock (`SELECT pg_advisory_lock(888999)`), guaranteeing forward-only, idempotent migration execution across concurrent service instances. Strict `pgx.ErrNoRows` inspection ensures transient DB failures are never treated as unapplied migrations.
7. **Test Database Isolation**:
   - Automated tests run against a dedicated `wallet_test_db`, isolated from application development databases.
8. **Least-Privileged Application Roles & Non-Development Environment Enforcement**:
   - Why we require explicit `DATABASE_URL` configuration in non-development environments (`production`, `staging`): eliminates accidental production runs with hardcoded development credentials or attempts to connect to default local hosts.
   - Why the local development fallback connects via a dedicated `wallet_app` role rather than the PostgreSQL superuser (`postgres`): limits the attack surface and prevents accidental superuser actions (`pg_read_file`, `DROP DATABASE`, configuration alters). Schema scripts and Docker Compose grant only required table and sequence DML/DDL permissions.
9. **Migration Advisory Lock Defer Ordering & Pool Connection Safety**:
   - Registering the `pg_advisory_unlock` defer immediately upon acquiring `lockConn` before attempting `SELECT pg_advisory_lock($1)` guarantees that if the lock is granted by PostgreSQL but the client context is canceled or encounters network disruption, the deferred unlock runs with `context.Background()` before `lockConn.Release()` returns the connection to the pool. This eliminates the risk of returning a connection holding the migration lock back to the pool, which would cause subsequent migrations to block indefinitely.
10. **System Treasury Protection & Administrative Funding Isolation**:
    - Why `system_treasury` is strictly restricted in public transfers: direct peer-to-peer transfers on `POST /transfers` or `TransferService.ExecuteTransfer` reject requests referencing `system_treasury` as source or destination (`ErrSystemTreasuryRestricted`, HTTP `403 Forbidden`), completely preventing unauthenticated attackers from draining or tampering with treasury funds.
    - Initial wallet funding on the public router `POST /wallets` rejects positive balances (`initialBalance > 0`) unless administrative authorization (`X-Admin-Key` / Bearer token) is supplied (`domain.ErrUnauthorizedFunding`, HTTP `403 Forbidden`). Public callers can only create unseeded wallets with balance 0.
    - Dedicated `POST /admin/wallets` endpoint provides an authorized path for initial balance seeding (`401 Unauthorized` without credentials).
    - Programmatic service invocations (`walletService.CreateWallet`) preserve out-of-band balance seeding for test fixtures and database migrations, guarded against public context leaks by `service.ContextWithPublicCaller`.
    - `AdminKey` is loaded via `internal/config/config.go` and is strictly required in non-development environments, defaulting to `"admin-secret-dev"` in local development and tests.
11. **Direct Database Trigger & Constraint Integration Assertions**:
    - Why application-level validation is insufficient without database-level trigger verification: application logic could theoretically have bugs or be bypassed by manual SQL or scripts.
    - Dedicated integration test suite directly exercises PostgreSQL triggers:
      - `TestDatabaseTriggers_RejectLedgerMutation_UpdateAndDelete`: asserts that raw SQL `UPDATE` and `DELETE` queries targeting `ledger_entries` are rejected by `trg_prevent_ledger_mutation` with `"ledger entries are immutable: deletions and updates are forbidden"`.
      - `TestDatabaseConstraints_RejectDuplicateLedgerEntryType`: asserts that duplicate entry types (e.g. 2 `DEBIT` records for the same transfer) fail immediately under unique index `idx_ledger_entries_transfer_type`.
      - `TestDatabaseTriggers_MalformedLedgerPairFailsAtCommit`: asserts that single debits, single credits, mismatched source wallets, mismatched destination wallets, and differing debit/credit amounts fail at transaction `Commit()` under deferred constraint trigger `trg_check_ledger_pair` with `"invalid ledger pair for transfer"`.
      - `TestDatabaseTriggers_ProcessedTransferWithoutLedgerPairFailsAtCommit`: asserts that committing a transfer with `status = 'PROCESSED'` without balanced ledger entries fails at `Commit()` under deferred constraint trigger `trg_check_transfer_processed` with `"cannot commit PROCESSED transfer"`.
12. **Strict Positive Debit Predicate**:
    - Why `CanDebit(amount)` strictly requires `amount > 0 && w.Balance >= amount`: aligns with `CanCredit` and `Debit` validation, preventing callers using `CanDebit` from approving zero or negative debit requests.
13. **Bounded Ledger History Pagination**:
    - Why `GetLedgerByWalletID` enforces bounded limits (default 50, maximum 100) and offset parameters: unbounded materialization of historical financial rows would lead to memory bloat, high latency, and request timeouts for long-lived active wallets.
14. **Safe-by-Default Configuration Security**:
    - Why omitted `APP_ENV` defaults to `production` and requires explicit `DATABASE_URL` and `ADMIN_KEY`: avoids running in production with accidental development fallback credentials (`admin-secret-dev`), protecting sensitive admin endpoints.
15. **Complete Owner-Token Lease Fencing**:
    - Why all `UpdateIdempotency` calls must require a non-empty `OwnerToken` with `owner_token = $6` in the SQL predicate: eliminating un-fenced update fallbacks guarantees that no stale, expired, or non-leaseholding worker can overwrite or tamper with an active idempotency reservation.
16. **Atomic Pending-to-Terminal Transfer Transitions**:
    - Why `UpdateTransferStatus` enforces `WHERE id = $4 AND status = 'PENDING'`: once a transfer reaches a terminal state (`PROCESSED` or `FAILED`), it is immutable. Guarding the update with `status = 'PENDING'` and returning `domain.ErrInvalidStateTransition` if 0 rows are updated prevents concurrent retries, background workers, or timeout handlers from corrupting or overwriting terminal outcomes.
17. **Commit-Time Transfer Status Database Triggers**:
    - Why `trg_check_ledger_pair` verifies that the transfer's status is `PROCESSED` at commit time: ensures that a transaction cannot commit ledger entries for a transfer marked `FAILED` or left as `PENDING`.
18. **Separation of Schema Definition from Role Provisioning**:
    - Why role creation statements are kept in `scripts/init-user.sql` rather than `000001_init_schema.sql`: in cloud production environments, database users and roles are managed by DBAs or infrastructure-as-code (Terraform/IAM) with least privilege (`SELECT, INSERT` on ledger, no `UPDATE`/`DELETE`). Application migration scripts should define tables, triggers, and indexes without hardcoding role creation.
19. **Connection Pool Headroom Reservation & Transaction Bounding**:
    - Why we reserve connection headroom (`pool.MaxConns - HeartbeatHeadroom`) and gate active transactions with a counting semaphore (`txSem`): if all connections in `pgxpool` were consumed by transactions, concurrent in-flight heartbeats would be starved of connections. Starved heartbeats would trigger false consecutive failure timeouts and cause self-inflicted transaction cancellations. Transaction bounding guarantees dedicated connections remain available for heartbeats and health checks under high load.
20. **Terminal-Only Stale Idempotency Recovery & Lookup Error Propagation**:
    - Why stale recovery only caches and finalizes transfers with status `PROCESSED` or `FAILED`: recovering `PENDING` transfers as completed would report HTTP 201 and finalize idempotency records before the transaction has committed or settled. Non-terminal transfers return `domain.ErrIdempotencyInProgress`.
    - Why `GetTransferByIdempotencyKey` lookup errors are propagated: reclaiming an idempotency lease when a lookup fails due to a database/network error could execute a duplicate transfer while the original is still committing. Reclaim is permitted only after an explicit `ErrTransferNotFound` or `pgx.ErrNoRows`.
21. **Strict Removal of Un-Locked Volatile Preflights**:
    - Why volatile checks like `CanCredit` are removed prior to acquiring row locks: between an un-locked preflight check and the transaction's row-lock acquisition, concurrent transactions can modify wallet balances. Releasing the idempotency reservation on a volatile preflight rejection allows racing requests to bypass idempotency safeguards. All balance limit and currency validations must be performed exclusively under `SELECT ... FOR UPDATE` row locks inside the ACID transaction.
22. **PostgreSQL 16 Schema Privileges for Least-Privileged Application Roles**:
    - Why `ALL ON SCHEMA public` is granted to `wallet_app`: PostgreSQL 16 restricts public schema permissions by default; `USAGE` alone allows reading existing objects but rejects DDL (`CREATE TABLE IF NOT EXISTS schema_migrations`, etc.). Granting schema creation privileges to `wallet_app` enables migration runners to boot cleanly without needing superuser privileges.
23. **Destructive Test DB Safeguards**:
    - Why `SetupTestDB` explicitly validates database names and rejects `wallet_db`, `postgres`, and databases matching application `DATABASE_URL`: prevents accidental drops (`DROP DATABASE ... WITH (FORCE)`) or table truncations if `TEST_DATABASE_URL` is misconfigured or points to an application database.
    - Test database connection error logs sanitize endpoints (`host/path`), preventing password leaks in CI or local output.
24. **Dedicated Heartbeat Connection Pool & Physical Pool Isolation**:
    - Why heartbeats use a dedicated connection pool (`postgres.NewHeartbeatPool`): even with transaction concurrency bounding on the main pool, high read volumes (wallet lookups, balance reconciliations, health checks) could consume all primary pool connections. By routing heartbeats through a physically separate 5-connection pool, background lease renewals are guaranteed instant connection acquisition and can never be starved into self-inflicted transaction cancellations.
25. **Single-Connection Pool (`MaxConns = 1`) Migration Deadlock Elimination**:
    - Why `Migrate` runs all table checks, DDL statements, and transactions through `lockConn`: on resource-constrained or single-connection databases (`MaxConns = 1`), calling `pool.Exec` or `pool.Begin` while `lockConn` holds an advisory lock would deadlock indefinitely waiting for a connection from the exhausted pool.
    - Deferred `tx.Rollback(rollbackCtx)` immediately after `lockConn.Begin(ctx)` ensures uncommitted migration transactions release catalog locks without relying on pool connection close.
26. **Database Invariant Enforcement: Terminal Immutability & Ledger Isolation**:
    - Why `check_transfer_processed_ledger_pair` disallows updates mutating `OLD.status IN ('PROCESSED', 'FAILED')`: ensures terminal transfer outcomes are immutable at the database boundary and cannot be rewritten by rogue queries or concurrent workers.
    - Why non-`PROCESSED` transfers (e.g. `PENDING` or `FAILED`) cannot have ledger entries at commit: prevents partial or unfinalized transfers from committing unbalanced ledger rows into the financial audit log.
27. **Idempotency Reservation Race Resilience & Bounded Cleanup**:
    - Why `ReserveIdempotency` retries in a bounded loop upon insert conflict when the conflicting row disappears: concurrent rollback or cleanup between the failed insert and subsequent lookup is treated as an immediate acquisition opportunity rather than an internal error.
    - Why `ExecuteTransfer` initiates a bounded background `DeleteInProgress` on reservation errors: prevents leaving orphaned `IN_PROGRESS` records if context cancellation or connection drops occur during key reservation.
28. **Least-Privilege Execution in CI**:
    - Why CI runs integration tests using `wallet_app:wallet_app_password` on `wallet_test_db`: verifies that migrations, table operations, constraint triggers, truncations, and repository queries all succeed under non-superuser privileges, preventing deployment failures caused by missing runtime role grants.
29. **Unconditional Current UTC Timestamp on Idempotency Updates**:
    - Why `UpdateIdempotency` unconditionally sets `updated_at = time.Now().UTC()`: `ReserveIdempotency` initializes `record.UpdatedAt` at reservation time. Terminal finalization callers pass the in-memory record back upon completion. If `UpdateIdempotency` preserved `record.UpdatedAt`, completed and failed records would retain their initial reservation timestamp instead of the finalization time, and a long-running transfer would overwrite newer background heartbeat timestamps with stale reservation timestamps. Setting `updated_at` to the current UTC time guarantees non-decreasing, accurate audit and finalization timestamps.
