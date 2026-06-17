# Wallet Transfer Service

## Overview

This project implements a wallet-to-wallet transfer service with a focus on:

* Idempotent request handling
* Double-entry ledger accounting
* Transactional consistency
* Concurrency safety
* Clean layered architecture

The solution is built using:

* Java 17
* Spring Boot 3.x
* PostgreSQL
* Spring Data JPA
* Flyway

---

## Architecture

The application follows a layered architecture:


Controller Layer
    ↓
Service Layer
    ↓
Repository Layer
    ↓
PostgreSQL


### Controller Layer

Responsible for:

* Request validation
* Request/response mapping
* Invoking service methods

### Service Layer

Responsible for:

* Transfer workflow orchestration
* Business rules
* Idempotency handling
* Ledger creation

### Repository Layer

Responsible for:

* Database access
* Entity persistence

---

## Database Schema

### wallets

Stores wallet balances.

| Column  | Type          |
| ------- | ------------- |
| id      | UUID          |
| balance | NUMERIC(19,2) |
| version | BIGINT        |

The version column is used for optimistic locking.

### transfers

Stores transfer requests and their state.

| Column                | Type          |
| --------------------- | ------------- |
| id                    | UUID          |
| source_wallet_id      | UUID          |
| destination_wallet_id | UUID          |
| amount                | NUMERIC(19,2) |
| status                | VARCHAR       |
| idempotency_key       | VARCHAR       |
| created_at            | TIMESTAMP     |

### ledger_entries

Stores accounting records.

| Column      | Type          |
| ----------- | ------------- |
| id          | UUID          |
| transfer_id | UUID          |
| wallet_id   | UUID          |
| amount      | NUMERIC(19,2) |
| entry_type  | DEBIT/CREDIT  |
| created_at  | TIMESTAMP     |

---

## Transfer State Machine

Supported states:

text
PENDING
PROCESSED
FAILED


Allowed transitions:

text
PENDING → PROCESSED
PENDING → FAILED


Every transfer is initially created in the `PENDING` state.

After successful wallet updates and ledger creation, the transfer moves to `PROCESSED`.

Any unexpected failure results in a `FAILED` state.

---

## Idempotency Strategy

Each transfer request requires an `Idempotency-Key` header.

Example:

http
Idempotency-Key: transfer-001


The key is stored with the transfer record and enforced using a unique database constraint.

Workflow:

1. Check for existing transfer by idempotency key.
2. If found, return the original result.
3. If not found, execute the transfer.
4. Store the key with the completed transfer.

This guarantees that duplicate requests do not create duplicate transfers.

---

## Double Entry Ledger

Every successful transfer creates exactly two ledger entries:

text
DEBIT  -> Source Wallet
CREDIT -> Destination Wallet

Example:

text
Transfer Amount = 100

Wallet A -> DEBIT 100
Wallet B -> CREDIT 100


Both entries reference the same transfer identifier.

This guarantees ledger consistency.

---

## Concurrency Handling

Wallet balances are protected using optimistic locking.

java
@Version
private Long version;


When concurrent updates occur:

1. JPA increments the version column.
2. Conflicting updates fail with an optimistic locking exception.
3. Lost updates and double-spending are prevented.

---

## Transaction Strategy

Transfers execute within a single transactional boundary.

java
@Transactional


The following operations are committed atomically:

* Debit source wallet
* Credit destination wallet
* Create transfer record
* Create ledger entries

If any operation fails, the transaction is rolled back.

---

## API Endpoints

### Create Transfer

http
POST /transfers


### Get Wallet

http
GET /wallets/{walletId}


### Transfer History

http
GET /wallets/{walletId}/transfers


---

## Testing

Implemented tests cover:

* Successful transfer execution
* Insufficient balance handling
* Idempotency behavior
* Ledger entry creation

---

## Assumptions

* Wallets are pre-created.
* Transfer amount must be positive.
* Source and destination wallets must be different.
* PostgreSQL is the primary database.

---

## Tradeoffs

The implementation uses optimistic locking instead of pessimistic locking.

Advantages:

* Better throughput
* Simpler implementation
* Suitable for low to medium contention workloads

For extremely high contention systems, row-level locking could be considered.

---

## AI Usage

Tool Used:

* ChatGPT

Usage:

* Design validation
* Schema review
* Code review assistance
* Test case brainstorming
