# Integration test documentation

This document explains the API integration tests. Unlike unit tests, these boot the full Spring context, run Flyway migrations against an in-memory H2 database (`MODE=PostgreSQL`), and exercise real HTTP endpoints through `MockMvc` with real JPA repositories and services.

## How they differ from unit tests

| | Unit tests | Integration tests |
|---|---|---|
| Spring context | No / slice (`@WebMvcTest`) | Full (`@SpringBootTest`) |
| Database | Mocked | Real H2 + Flyway schema |
| Services / repos | Mocked | Real beans |
| Focus | Isolated logic / HTTP mapping | End-to-end persistence, transactions, idempotency, concurrency |

## Shared base

**File:** `src/test/java/com/robustrade/wallet/integration/AbstractIntegrationTest.java`

- `@SpringBootTest` + `@AutoConfigureMockMvc` + `@ActiveProfiles("test")`
- Profile config: `src/test/resources/application-test.yml`
- `@BeforeEach` truncates `ledger_entries` → `idempotency_records` → `transfers` → `wallets`
- Helpers: create wallet/transfer, fetch statement, read balances/states from JDBC

## WalletApiIntegrationTest

**File:** `src/test/java/com/robustrade/wallet/integration/WalletApiIntegrationTest.java`

### 1. `createWallet_persistsAndReturnsCreatedWallet`
Creates a wallet via `POST /wallets` and asserts response fields plus DB balance.

### 2. `createWallet_whenUserAlreadyHasWallet_returnsConflict`
Second create for the same `userId` → `409 WALLET_ALREADY_EXISTS`.

### 3. `createWallet_whenBalanceNegative_returnsBadRequest`
Negative balance → `400 VALIDATION_ERROR`.

## TransferApiIntegrationTest

**File:** `src/test/java/com/robustrade/wallet/integration/TransferApiIntegrationTest.java`

### 1. `createTransfer_movesFundsWritesLedgerAndMarksSuccess`
Happy path: balances updated, transfer `PROCESSED`, 2 ledger rows (DEBIT + CREDIT).

### 2. `createTransfer_whenWalletMissing_returnsNotFound`
Unknown destination wallet → `404 WALLETS_NOT_FOUND`.

### 3. `createTransfer_whenInsufficientBalance_marksFailedAndLeavesBalancesUnchanged`
Amount exceeds source balance → `400 INSUFFICIENT_BALANCE`, balances unchanged, transfer `FAILED`, no ledger rows.

### 4. `createTransfer_whenSameIdempotencyKeyAndPayload_returnsSameTransferWithoutDoubleDebit`
Replay with same key + payload returns the same `transferId` and does not debit twice.

### 5. `createTransfer_whenSameIdempotencyKeyDifferentPayload_returnsConflict`
Same key, different amount → `409 IDEMPOTENCY_CONFLICT`; first transfer balances preserved.

### 6. `createTransfer_whenPreviouslyFailed_retriesAndSucceedsAfterTopUp`
Failed transfer is retried after topping up the source wallet via SQL; ends `PROCESSED` with ledger rows.

### 7. `createTransfer_whenAmountInvalid_returnsValidationError`
Amount `0.00` → `400 VALIDATION_ERROR`.

### 8. `createTransfer_whenWalletsAreSame_returnsValidationError`
Same from/to wallet → `400 VALIDATION_ERROR`.

## StatementApiIntegrationTest

**File:** `src/test/java/com/robustrade/wallet/integration/StatementApiIntegrationTest.java`

### 1. `getStatement_byWalletId_afterTransfer_showsRunningBalances`
After a transfer, from/to statements show DEBIT/CREDIT with correct previous/after balances.

### 2. `getStatement_byUserId_returnsWalletStatement`
Lookup by `userId` for a single wallet.

### 3. `getStatement_whenNeitherParamProvided_returnsBadRequest`
No query params → `400 VALIDATION_ERROR`.

### 4. `getStatement_whenWalletMissing_returnsNotFound`
Unknown `walletId` → `404 WALLET_NOT_FOUND`.

### 5. `databaseRejectsSecondWalletForSameUserId`
Direct SQL insert of a second wallet for the same `userId` fails on unique index `wallets_user_id_uk` (one wallet per user).

### 6. `getStatement_whenWalletDoesNotBelongToUser_returnsBadRequest`
Mismatched `userId` + `walletId` → `400 WALLET_USER_MISMATCH`.

### 7. `getStatement_whenBothParamsMatch_returnsOk`
Matching `userId` and `walletId` → `200`.

## ConcurrentTransferIntegrationTest

**File:** `src/test/java/com/robustrade/wallet/integration/ConcurrentTransferIntegrationTest.java`

### 1. `concurrentTransfers_doNotOverdrawSourceWallet`
20 parallel transfers of `10.00` from a wallet with `100.00`:
- exactly 10 succeed (`201`)
- exactly 10 fail with `INSUFFICIENT_BALANCE`
- final balances `0` / `100`
- 20 ledger rows total (10 transfers × 2)

Validates pessimistic locking under concurrent load.

### 2. `endToEnd_createWalletsTransferAndReadStatements`
Two wallets, two transfers (A→B then B→A), then statement checks for chronological running balances.

## TransferResilienceIntegrationTest

**File:** `src/test/java/com/robustrade/wallet/integration/TransferResilienceIntegrationTest.java`

### Concurrency
1. `concurrentIdenticalIdempotencyKey_createsSingleTransferAndDebitsOnce` — parallel same key → all `201` with one transfer / one debit (unique-key losers recover via replay).
2. `concurrentOppositeDirectionTransfers_conserveTotalBalance` — parallel A↔B transfers conserve total funds.

### Safe state transitions
3. `markFailed_afterProcessed_doesNotOverwriteStateOrLedger` — late `markFailed` cannot clobber `PROCESSED` or ledger.
4. `markFailed_blockedBehindSuccessLock_doesNotOverwriteProcessed` — concurrent success holding `FOR UPDATE` wins; `markFailed` then sees `PROCESSED` and skips.
5. `processedTransfer_replayKeepsProcessedAndDoesNotChangeStateOrBalances` — PROCESSED stays PROCESSED on replay.
6. `failedTransfer_remainsFailedUntilSuccessfulRetry` — FAILED remains FAILED without top-up; no ledger rows.

### Retry-safe behavior
7. `retryAfterTopUp_isIdempotentOnFurtherReplays` — FAILED → top-up → PROCESSED → replay does not double-debit.
8. `concurrentRetriesOfFailedKey_afterTopUp_succeedOnce` — parallel retries after top-up share one PROCESSED.

### Correct balance tracking
9. `multiHopTransfers_statementBalancesMatchWalletAndConserveFunds` — A→B→C→A conserves total; statements match wallets.
10. `failedTransfer_doesNotAffectBalancesOrStatementEntries` — failed path leaves balances/entries untouched.

## How to run

Integration tests are included in the default Maven build (`./mvnw test`, `./mvnw clean verify`).

```bash
# Full service build (unit + integration)
./mvnw clean verify

# Integration tests only
./mvnw -Dtest='*IntegrationTest' test

# Individual classes
./mvnw -Dtest=WalletApiIntegrationTest test
./mvnw -Dtest=TransferApiIntegrationTest test
./mvnw -Dtest=StatementApiIntegrationTest test
./mvnw -Dtest=ConcurrentTransferIntegrationTest test
```

Uses the `test` profile and H2 — no Docker Postgres required for these tests.

## Implementation note

Integration tests uncovered a lock deadlock on insufficient-balance: `TransferStatusService.markFailed` (`REQUIRES_NEW`) cannot update a transfer row while the outer transaction still holds `SELECT … FOR UPDATE`. The service now marks `FAILED` **after** the transfer transaction rolls back and releases locks.
