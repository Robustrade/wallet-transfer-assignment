# Wallet Transfer Service

A Spring Boot service that supports wallet-to-wallet transfers with PostgreSQL persistence, idempotent request handling, transactional consistency, and double-entry ledger recording.

## Overview

The service provides:

* Wallet creation and balance retrieval
* Wallet-to-wallet fund transfers
* Idempotent transfer processing
* Double-entry ledger recording
* Protection against concurrent overspending

The implementation is designed to ensure that a transfer is processed exactly once while maintaining consistent wallet balances and ledger records.

---

## Tech Stack

* Java 17
* Spring Boot 3
* Spring JDBC
* PostgreSQL
* Maven

---

## Project Structure

```text
src/main/java/com/viditec/wallettransfer
├── api
│   ├── controller
│   ├── request
│   └── response
├── config
├── domain
├── dto
├── exception
├── mapper
├── repository
└── service
```

---

## API Endpoints

### Create Wallet

```http
POST /wallets
```

Request

```json
{
  "walletId": "wallet_1",
  "initialBalance": 1000
}
```

---

### Get Wallet Balance

```http
GET /wallets/{walletId}
```

---

### Create Transfer

```http
POST /transfers
```

Request

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Example Success Response

```json
{
  "transferId": "trf_123",
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "state": "PROCESSED"
}
```

---

## Design Decisions

### Idempotency

Each transfer request includes an `idempotencyKey`.

An idempotency record is created before any balance changes occur.

If the same key is received again:

* The original response is returned when the payload matches.
* HTTP 409 is returned when the payload differs.

This guarantees that retries do not create duplicate transfers.

---

### Double-Entry Ledger

Each successful transfer creates:

* One DEBIT ledger entry
* One CREDIT ledger entry

Ledger records are immutable and provide an auditable history independent of wallet balances.

---

### Concurrency Handling

The primary concurrency concern is preventing multiple simultaneous debits from overspending the same wallet.

The implementation uses a conditional debit statement:

```sql
UPDATE wallets
SET balance = balance - :amount
WHERE wallet_id = :walletId
  AND balance >= :amount;
```

PostgreSQL automatically acquires a row-level lock during the update and re-evaluates the condition after waiting transactions are resumed.

This prevents double spending without requiring application-level locks.

---

### Transaction Boundaries

The following operations execute within a single database transaction:

1. Transfer creation
2. Wallet debit
3. Wallet credit
4. Ledger entry creation
5. Transfer state update
6. Idempotency completion

Any failure causes the transaction to roll back.

---

## Database Schema

The schema is initialized from:

```text
src/main/resources/schema.sql
```

Main tables:

* wallets
* transfers
* ledger_entries
* idempotency_records

Detailed design rationale is available in:

```text
docs/design-note.md
```

---

## Local Development

### Start PostgreSQL

```bash
docker compose up -d
```

Default configuration:

```text
Database: wallet_transfer
Username: postgres
Password: postgres
Port: 5432
```

---

## Configuration

The application reads:

```text
DB_URL
DB_USERNAME
DB_PASSWORD
```

Local defaults are provided in:

```text
src/main/resources/application.yml
```

---

## Run the Application

Linux / macOS

```bash
./mvnw spring-boot:run
```

Windows

```powershell
.\mvnw.cmd spring-boot:run
```

---

## Testing

Run all tests:

```bash
mvn test
```

The test suite covers:

* Successful transfers
* Insufficient funds
* Idempotency handling
* Ledger consistency
* Concurrent debit scenarios

---

## Future Improvements

Potential enhancements include:

* PostgreSQL Testcontainers integration tests
* Transfer history APIs
* Metrics and distributed tracing
* Transfer reversal support
* Reconciliation jobs
* Recovery handling for incomplete idempotency records
