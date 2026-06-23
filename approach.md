# Understanding and Approach - Wallet Transfer Service

This document outlines the architectural decisions, database design, idempotency strategy, and concurrency controls proposed for the Wallet Transfer Service.

---

## 1. Problem Understanding & Requirements

The task requires building a reliable wallet-to-wallet transfer backend service. The key challenges are:
- **Atomicity**: Either both wallets are updated and ledger entries written, or none.
- **Idempotency**: Providing exactly-once semantics. If a request is retried (e.g., due to network issues), it must return the original response without executing the transaction again.
- **Concurrency Safety**: Avoiding race conditions, double spending, and deadlocks when multiple transactions happen on the same wallets at the same time.
- **Double-Entry Ledger consistency**: Debit and credit entries must always match the transfer amount and must balance.

---

## 2. Approach & Architecture

We will implement a **Clean Layered Architecture** using **Java 21**, **Spring Boot**, and **Spring Data JPA** with an **H2 Database** (configured in PostgreSQL compatibility mode for zero-setup execution).

### A. Directory/Package Structure
```text
src/main/java/com/example/wallet/
├── config/              # Configuration (Swagger, Exception handling, Jackson)
├── domain/              # Entities (Wallet, Transfer, LedgerEntry, IdempotencyRecord)
├── repository/          # Spring Data JPA Repositories
├── service/             # Services (TransferService, IdempotencyService)
├── web/                 # REST Controllers, DTOs (TransferRequest, TransferResponse)
└── WalletApplication.java
```

### B. Database Schema & Models
1. **`Wallet`**:
   - `id` (String): Unique identifier.
   - `balance` (BigDecimal): Holds wallet balance (using Decimal 19,4 to prevent floating-point issues).
   - `createdAt` & `updatedAt` (Timestamp).
2. **`Transfer`**:
   - `id` (String/UUID).
   - `fromWalletId` & `toWalletId` (String).
   - `amount` (BigDecimal).
   - `status` (Enum: `PENDING`, `PROCESSED`, `FAILED`).
   - `errorMessage` (String).
   - `idempotencyKey` (String).
3. **`LedgerEntry`**:
   - `id` (Long, auto-increment).
   - `walletId` & `transferId` (String).
   - `type` (Enum: `DEBIT`, `CREDIT`).
   - `amount` (BigDecimal).
4. **`IdempotencyRecord`**:
   - `idempotencyKey` (String, PK).
   - `statusCode` (Integer).
   - `responseBody` (String).

---

## 3. Concurrency Strategy

To prevent double spending and ensure thread safety under concurrent requests:
1. **Pessimistic Write Locking**:
   - When executing a transfer, we will load the source and destination wallets using `@Lock(LockModeType.PESSIMISTIC_WRITE)`.
   - This translates to `SELECT ... FOR UPDATE` at the database level, preventing other transactions from reading or updating the balance of these wallets until the current transaction completes.
2. **Deadlock Prevention**:
   - If two transfers happen concurrently between `wallet_A` and `wallet_B` in opposite directions, a deadlock can occur if transaction 1 locks A and waits for B, while transaction 2 locks B and waits for A.
   - **Solution**: We sort the wallet IDs lexicographically. We always acquire the lock on the wallet with the smaller ID first, then the larger ID. This enforces a consistent lock acquisition order and completely eliminates deadlocks.

---

## 4. Idempotency Strategy

To guarantee exactly-once processing:
1. We store the result of completed requests in `idempotency_records` using the `idempotencyKey` as the primary key.
2. When a transfer request arrives:
   - Check if `idempotencyKey` exists.
   - If found, return the stored HTTP status code and response body.
   - If not found, execute the transfer inside a `@Transactional` block. The transaction will:
     - Check/lock wallets, validate funds, perform balance updates, write ledger entries, write the transfer record, and save the `IdempotencyRecord` with the response status/body.
     - If another request with the same key arrives while the transaction is active, the database unique index on `idempotency_records(idempotency_key)` will throw a unique constraint violation. We catch this and return a `409 Conflict` (or poll for completion).

---

## 5. Testing & Verification

We will write:
- **Unit/Service Tests**: Verify normal transfers, insufficient funds, invalid inputs, and ledger balance checks.
- **Idempotency Tests**: Verify replay behavior (verifying the exact same response is returned on retry, and ledger entries are not duplicated).
- **Concurrency Tests**: Use multi-threaded tests (`ExecutorService`) to run concurrent debit requests on a single wallet to ensure balance correctness.
