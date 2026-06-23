# Wallet Transfer Service

A robust, transactional backend service built using **Spring Boot 3.x** and **Java 21** that supports wallet-to-wallet transfers. 

This implementation prioritizes exactly-once API semantics (idempotency), database consistency (double-entry ledger), and safe concurrent execution (deadlock prevention & locking).

---

## 🛠️ Technology Stack
- **Language**: Java 21
- **Framework**: Spring Boot 3.x, Spring Data JPA
- **Database**: H2 Database (configured in PostgreSQL compatibility mode for zero-setup execution)
- **Build System**: Maven (wrapper included)

---

## 📐 Architecture & Design Decisions

### 1. Database Schema
The database uses H2 (in-memory) with schemas created automatically on startup. The schema includes four main tables:
- **`wallets`**: Contains the account balance with a database constraint (`balance >= 0`) ensuring no wallet can ever drop below zero.
- **`transfers`**: Records each transfer request, its state (`PENDING`, `PROCESSED`, `FAILED`), and any error messages.
- **`ledger_entries`**: A double-entry ledger recording all debits and credits. Every transfer generates exactly two entries (one debit and one credit) to ensure the system balances.
- **`idempotency_records`**: Stores API response bodies and HTTP status codes mapped to client-provided idempotency keys.

### 2. Concurrency & Deadlock Prevention
To handle concurrent requests safely without race conditions or double spending:
- **Pessimistic Write Locking**: We acquire a database-level lock (`SELECT ... FOR UPDATE` via JPA `@Lock(LockModeType.PESSIMISTIC_WRITE)`) on the source and destination wallets before validating balances and executing the transfer.
- **Deadlock Avoidance**: When locking two wallets, we sort their IDs lexicographically. We always lock the wallet with the smaller ID first, then the larger ID. This prevents circular dependencies (deadlocks) when concurrent transactions attempt transfers between the same accounts in opposite directions.

### 3. Idempotency (Exactly-Once Semantics)
- When a request is received, the service checks `idempotency_records` for the key.
- If it exists, the cached response is instantly returned.
- If it doesn't exist, the transaction is executed. The database enforces a `UNIQUE` constraint on the idempotency key. Any concurrent duplicate request trying to execute at the same microsecond will fail the DB insert, throwing a unique constraint violation which we catch and resolve by returning the correct processed result.

---

## 🚀 Running the Application

### Prerequisites
- **Java 21** installed on your system.

### Steps to Run
1. Navigate to the project root directory.
2. Build and run the server:
   ```bash
   .\mvnw.cmd spring-boot:run
   ```
3. Once started, the server listens on port `8080`.

---

## 🧪 Testing

We have built a thorough test suite covering edge cases, state transitions, validation, and multi-threaded concurrency.

To execute the test suite:
```bash
.\mvnw.cmd clean test
```

### Tests Overview:
- **`WalletApplicationTests`**: Verifies application context loading.
- **`TransferConcurrencyTest`**: Spawns multiple concurrent threads attempting to transfer money from the same source wallet simultaneously to verify thread-safety and prevent double spending.
- **`TransferControllerTest`**: Asserts the REST endpoints, validation errors, non-existent wallet handling, and idempotency replay behavior.

---

## 📬 API Specification

### Create Transfer
- **Endpoint**: `POST /transfers`
- **Headers**: `Content-Type: application/json`
- **Request Body**:
  ```json
  {
    "idempotencyKey": "unique-uuid-key",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 150.00
  }
  ```

- **Successful Response (201 Created)**:
  ```json
  {
    "transferId": "48b625ca-d84d-4523-a55e-990c0ef48e24",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 150.00,
    "status": "PROCESSED",
    "errorMessage": null
  }
  ```

- **Insufficient Funds Response (422 Unprocessable Entity)**:
  ```json
  {
    "transferId": "9127814b-22fb-4e1b-b27a-85d8d1e21b24",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 5000.00,
    "status": "FAILED",
    "errorMessage": "insufficient funds"
  }
  ```

---

## 🖥️ Database Verification (H2 Console)
While the application is running, you can inspect the database directly:
- **Console URL**: [http://localhost:8080/h2-console](http://localhost:8080/h2-console)
- **JDBC URL**: `jdbc:h2:mem:walletdb`
- **Username**: `sa`
- **Password**: *(leave blank)*

---

## 🤖 AI Usage Disclosure

As requested by the assignment guidelines:

1. **Tool Used**: Antigravity (Advanced Agentic Coding assistant developed by Google DeepMind).
2. **Usage Strategy**: Antigravity was used as a senior developer pair-programmer. It assisted in analyzing the initial workspace, creating the architecture and design plan, verifying compile success and Maven wrapper commands, and structuring the end-to-end execution guide.
3. **Session Transcript**: The full interaction history and prompts are maintained locally in the system generated logs, and a walkthrough log is provided in the repository under [walkthrough.md](./walkthrough.md).
