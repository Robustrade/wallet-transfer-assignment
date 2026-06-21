# Wallet Transfer Assignment

A Spring Boot backend service for wallet-to-wallet fund transfers.

This implementation focuses on correctness around wallet balance updates, idempotent request handling, double-entry ledger creation, and safe concurrent transfer execution.

---

## Tech Stack

* Java 21
* Spring Boot
* Spring Data JPA
* PostgreSQL
* Maven
* JUni
---

## Main Features

* Wallet-to-wallet transfer API
* Idempotency support using `idempotencyKey`
* Pessimistic locking for safe concurrent wallet updates
* Transfer lifecycle using `PENDING`, `PROCESSED`, and `FAILED`
* Double-entry ledger entries for successful transfers
* Integration tests for success, failure, duplicate requests, and concurrency cases

---

## Database Design

The implementation uses four main entities/tables:

### `wallet`

Stores wallet balance.

Important fields:

* `wallet_id`
* `balance`

The wallet balance is expected to remain non-negative.

### `transfer`

Stores each transfer attempt.

Important fields:

* `transfer_id`
* `source_wallet_id`
* `destination_wallet_id`
* `amount`
* `status`

Transfer status values:

* `PENDING`
* `PROCESSED`
* `FAILED`

### `ledger_entry`

Stores debit and credit records for successful transfers.

Important fields:

* `entry_id`
* `transfer_id`
* `wallet_id`
* `amount`
* `transaction_type`

Transaction type values:

* `DEBIT`
* `CREDIT`

For every successful transfer, exactly two ledger entries are created:

1. One debit entry for the source wallet.
2. One credit entry for the destination wallet.

### `idempotency_record`

Stores request-processing state for retry-safe behavior.

Important fields:

* `idempotency_record_id`
* `idempotency_key`
* `status`
* `transfer_id`
* `result_status`
* `result_message`

The `idempotency_key` is unique, so the same request key cannot trigger the transfer flow more than once.

---

## Idempotency Strategy

The service uses an `idempotency_record` table to avoid duplicate transfer processing.

Flow:

1. A request comes with an `idempotencyKey`.
2. The service tries to claim the key using a unique insert.
3. If the key is new, the request proceeds.
4. If the key already exists, the transfer logic is not executed again.
5. The previously stored result/status is returned.
6. For expected business failures, such as insufficient balance, the failed result is also stored so retries return the same result.

This makes the transfer operation retry-safe for duplicate requests or network retries.

---

## Transaction Strategy

The main transfer flow runs inside a database transaction.

Within the transaction:

1. The idempotency key is claimed.
2. A transfer record is created with `PENDING` status.
3. Source and destination wallet rows are locked.
4. Source wallet balance is validated.
5. Source wallet is debited.
6. Destination wallet is credited.
7. Debit and credit ledger entries are inserted.
8. Transfer is marked as `PROCESSED`.
9. Idempotency result is updated.

For expected business failures, the transfer is marked as `FAILED` and committed with the idempotency result.

For unexpected technical failures, the transaction is rolled back to avoid partial wallet updates or partial ledger entries.

---

## Concurrency Strategy

The implementation uses pessimistic locking with PostgreSQL `FOR UPDATE`.

The source and destination wallet rows are locked before balance updates. Wallets are fetched in wallet ID order to reduce deadlock risk.

This ensures that concurrent transfers from the same source wallet are handled safely. One transaction completes first, and the next transaction sees the updated balance before proceeding.

---

## API

### Create Transfer

```http
POST /transfers
Content-Type: application/json
```

### Request Body

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": 1,
  "toWalletId": 2,
  "amount": 100.00
}
```

### Success Response

```json
{
  "transferId": 1,
  "fromWalletId": 1,
  "toWalletId": 2,
  "amount": 100.00,
  "status": "PROCESSED",
  "message": "Transfer processed successfully"
}
```

### Failed Response Example

```json
{
  "transferId": 2,
  "fromWalletId": 1,
  "toWalletId": 2,
  "amount": 100.00,
  "status": "FAILED",
  "message": "Insufficient balance"
}
```

---

## Running the Application

From the project root:

```bash
mvn spring-boot:run
```

Or on Windows with Maven wrapper:

```bash
.\mvnw.cmd spring-boot:run
```

The application starts on:

```text
http://localhost:8080
```

---

## Running Tests

Run:

```bash
mvn test
```

Or on Windows with Maven wrapper:

```bash
.\mvnw.cmd test
```

---

## Tests Covered

The test suite covers:

* Successful wallet transfer
* Insufficient balance
* Invalid wallet scenario
* Duplicate idempotency key
* Ledger entry validation
* Concurrent transfer attempts from the same wallet

---

## AI Usage

AI usage is documented separately in:

```text
AI_USAGE.md
```

I used ChatGPT as a planning, review, and debugging assistant. I did not use AI to blindly generate the full solution.