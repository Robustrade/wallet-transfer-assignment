# Unit test documentation

This document explains the unit tests for wallet, transfer, and statement APIs.

## WalletServiceImplTest

**File:** `src/test/java/com/robustrade/wallet/service/WalletServiceImplTest.java`  
**Style:** Pure unit tests with Mockito (`@ExtendWith(MockitoExtension.class)`)  
**Class under test:** `WalletServiceImpl`  
**Mocked dependency:** `WalletRepository`

### Setup

- `@Mock WalletRepository` — fake repository; no real DB
- `@InjectMocks WalletServiceImpl` — real service with mocked repo injected

---

### Test 1: `createWallet_whenUserHasNoWallet_savesAndReturnsResponse`

**Intent:** Happy path — create a wallet when the user does not already have one.

**Given**
- Request: `userId=1001`, `balance=1000.00`
- `existsByUserId(1001)` returns `false`
- `save(...)` returns the same wallet entity that was passed in

**When**
- `walletService.createWallet(request)` is called

**Then**
- Response contains matching `userId`, `balance`, non-blank `walletId`, and `createdAt`
- Repository `existsByUserId(1001)` was called
- Repository `save(...)` was called with a wallet that has:
  - `userId=1001`
  - `balance=1000.00`
  - `id` equal to the response `walletId`

**Why `ArgumentCaptor`?**  
It captures the exact `Wallet` passed to `save(...)` so we assert the persisted fields, not only that save happened.

---

### Test 2: `createWallet_whenWalletAlreadyExists_throwsConflict`

**Intent:** Reject duplicate wallet creation for the same user.

**Given**
- Request: `userId=1001`, `balance=500.00`
- `existsByUserId(1001)` returns `true`

**When**
- `walletService.createWallet(request)` is called

**Then**
- Throws `WalletServiceException`
- `errorCode = WALLET_ALREADY_EXISTS`
- `httpStatus = 409`
- Message contains `1001`
- `save(...)` is **never** called

---

## TransferServiceImplTest

**File:** `src/test/java/com/robustrade/wallet/service/TransferServiceImplTest.java`  
**Style:** Pure unit tests with Mockito  
**Class under test:** `TransferServiceImpl`  
**Mocked dependencies:** `WalletRepository`, `TransferRepository`, `LedgerEntryRepository`, `IdempotencyRecordRepository`, `TransferStatusService`, `TransactionTemplate`

### Setup notes

- `TransactionTemplate.execute(...)` is stubbed to run the callback immediately (no real transaction manager).
- Saved transfers are kept in an `AtomicReference` so later `findByIdForUpdate` returns the same entity.

---

### Test 1: `createTransfer_whenNewRequest_completesSuccessfully`

**Intent:** Happy path for a new transfer.

**Given**
- Both wallets exist; from balance `500`, to balance `50`
- No existing idempotency key

**Then**
- Response state is `PROCESSED`
- From balance becomes `400`, to balance becomes `150`
- Transfer, idempotency record, both wallets, and 2 ledger entries are saved

---

### Test 2: `createTransfer_whenWalletsMissing_throwsNotFound`

**Intent:** Fail fast if source/destination wallet does not exist.

**Then**
- `WALLETS_NOT_FOUND`, HTTP 404
- No transfer / idempotency persistence

---

### Test 3: `createTransfer_whenInsufficientBalance_marksFailedAndThrows`

**Intent:** Debit is rejected when source wallet cannot cover the amount.

**Given**
- From wallet balance `40`, transfer amount `100`

**Then**
- `INSUFFICIENT_BALANCE`, HTTP 400
- `transferStatusService.markFailed(...)` is called
- No ledger entries are written

---

### Test 4: `createTransfer_whenIdempotencyKeyExistsWithSameHashAndSuccess_returnsExisting`

**Intent:** Exactly-once API behavior for a completed transfer.

**Given**
- Existing idempotency record with matching request hash
- Linked transfer already `PROCESSED`

**Then**
- Returns the existing transfer
- No wallet locks / ledger writes

---

### Test 5: `createTransfer_whenIdempotencyKeyExistsWithDifferentHash_throwsConflict`

**Intent:** Same idempotency key with a different payload is rejected.

**Then**
- `IDEMPOTENCY_CONFLICT`, HTTP 409

---

### Test 6: `createTransfer_whenUniqueKeyRace_recoversViaReplay`

**Intent:** Concurrent insert on the same idempotency key recovers by replaying the winner.

**Given**
- First lookup finds no key
- Insert hits unique constraint (`DataIntegrityViolationException`)
- Reload finds the winner’s idempotency record + `PROCESSED` transfer

**Then**
- Returns the winner’s transfer (`201` path in API)
- Increments `wallet.transfer.unique_key_races`
- No extra ledger writes

---

### Test 7: `createTransfer_whenIdempotencyKeyExistsAndFailed_retriesAndSucceeds`

**Intent:** RETRY flow for a previously `FAILED` transfer with the same payload.

**Given**
- Existing idempotency + `FAILED` transfer
- Source wallet now has enough balance

**Then**
- Transfer moves through retry and ends as `PROCESSED`
- Balances updated correctly

---

## TransferHandlerTest

**File:** `src/test/java/com/robustrade/wallet/handler/TransferHandlerTest.java`  
**Style:** Spring MVC slice test (`@WebMvcTest`)  
**Class under test:** `TransferHandler`  
**Mocked dependency:** `TransferService`  
**Imported:** `GlobalExceptionHandler`

---

### Test 1: `createTransfer_validRequest_returnsCreated`

**Intent:** `POST /transfers` with valid body → `201` and transfer JSON.

---

### Test 2: `createTransfer_whenWalletsNotFound_returnsNotFound`

**Intent:** Service `WALLETS_NOT_FOUND` → HTTP 404.

---

### Test 3: `createTransfer_whenIdempotencyConflict_returnsConflict`

**Intent:** Service `IDEMPOTENCY_CONFLICT` → HTTP 409.

---

### Test 4: `createTransfer_whenInsufficientBalance_returnsBadRequest`

**Intent:** Service `INSUFFICIENT_BALANCE` → HTTP 400.

---

### Test 5: `createTransfer_whenAmountInvalid_returnsBadRequest`

**Intent:** Bean validation rejects amount `< 0.01` before service is called.

---

### Test 6: `createTransfer_whenWalletsSame_returnsBadRequest`

**Intent:** Bean validation rejects same from/to wallet ids.

---

## StatementServiceImplTest

**File:** `src/test/java/com/robustrade/wallet/service/StatementServiceImplTest.java`  
**Style:** Pure unit tests with Mockito  
**Class under test:** `StatementServiceImpl`  
**Mocked dependencies:** `WalletRepository`, `LedgerEntryRepository`

---

### Test 1: `getStatement_byWalletId_returnsChronologicalEntriesWithRunningBalances`

**Intent:** Build statement by `walletId` with correct running balances.

**Given**
- Wallet `wallet-1` for user `1001` with current balance `900.00`
- Two ledger rows in order:
  1. CREDIT `1000.00`
  2. DEBIT `100.00`

**When**
- `getStatement(null, "wallet-1")`

**Then**
- Header fields: `walletId`, `userId`, `currentBalance=900.00`
- Entry 1: CREDIT, previous `0.00`, after `1000.00`
- Entry 2: DEBIT, previous `1000.00`, after `900.00`

Opening balance is reconstructed as:  
`currentBalance - credits + debits` → `900 - 1000 + 100 = 0`.

---

### Test 2: `getStatement_byUserId_whenSingleWallet_returnsStatement`

**Intent:** Resolve wallet by `userId` when exactly one wallet exists.

**Given**
- `findByUserId(1001)` returns one wallet
- No ledger entries

**Then**
- Statement returned for that wallet
- `entries` is empty

---

### Test 3: `getStatement_whenBothParamsProvidedAndMatch_usesWalletId`

**Intent:** When both `userId` and `walletId` are provided and match, use `walletId` lookup.

**Then**
- `findById(walletId)` is used
- Statement returns successfully

---

### Test 4: `getStatement_whenNeitherParamProvided_throwsValidationError`

**Intent:** At least one of `userId` / `walletId` is mandatory.

**Then**
- `VALIDATION_ERROR`, HTTP 400

---

### Test 5: `getStatement_whenWalletNotFound_throwsNotFound`

**Intent:** Unknown `walletId` fails clearly.

**Then**
- `WALLET_NOT_FOUND`, HTTP 404

---

### Test 6: `getStatement_whenUserHasMultipleWallets_throwsBadRequest`

**Intent:** Ambiguous `userId` when multiple wallets exist.

**Then**
- `MULTIPLE_WALLETS`, HTTP 400

---

### Test 7: `getStatement_whenWalletDoesNotBelongToUser_throwsMismatch`

**Intent:** Both params provided but wallet belongs to another user.

**Then**
- `WALLET_USER_MISMATCH`, HTTP 400

---

## StatementHandlerTest

**File:** `src/test/java/com/robustrade/wallet/handler/StatementHandlerTest.java`  
**Style:** Spring MVC slice test (`@WebMvcTest`)  
**Class under test:** `StatementHandler`  
**Mocked dependency:** `StatementService`  
**Imported:** `GlobalExceptionHandler` (maps service exceptions to HTTP responses)

---

### Test 1: `getStatement_byWalletId_returnsOk`

**Intent:** `GET /statements?walletId=...` returns 200 and statement JSON.

---

### Test 2: `getStatement_byUserId_returnsOk`

**Intent:** `GET /statements?userId=...` returns 200.

---

### Test 3: `getStatement_whenMissingLookup_returnsBadRequest`

**Intent:** No query params → service throws validation error → HTTP 400.

---

### Test 4: `getStatement_whenWalletNotFound_returnsNotFound`

**Intent:** Missing wallet → HTTP 404 with `WALLET_NOT_FOUND`.

---

### Test 5: `getStatement_whenMultipleWallets_returnsBadRequest`

**Intent:** Multiple wallets for user → HTTP 400 with `MULTIPLE_WALLETS`.

---

## How to run

Unit and integration tests are included in the default Maven build (`./mvnw test`, `./mvnw clean verify`).

```bash
# Full service build (runs all Surefire tests, including this suite)
./mvnw clean verify

# Wallet service tests
./mvnw -Dtest=WalletServiceImplTest test

# Transfer tests
./mvnw -Dtest=TransferServiceImplTest,TransferHandlerTest test

# Statement tests
./mvnw -Dtest=StatementServiceImplTest,StatementHandlerTest test

# Documented unit tests only
./mvnw -Dtest=WalletServiceImplTest,TransferServiceImplTest,TransferHandlerTest,StatementServiceImplTest,StatementHandlerTest test
```
