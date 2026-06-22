# Wallet Transfer Service - Design Document

## Problem Statement

The objective of this service is to implement a wallet-to-wallet transfer system that guarantees:

* Idempotent request processing
* Double-entry ledger consistency
* Safe concurrent execution
* Atomic transaction processing
* Retry-safe behavior
* Transfer state management

The system must behave correctly even when duplicate requests, retries, concurrent updates, and partial failures occur.

---

# Key Design Decisions

Before implementation, the following architectural decisions were made.

## Idempotency

A dedicated `idempotency_records` table is used.

Each request generates a SHA-256 hash derived from:

* Source Wallet
* Destination Wallet
* Amount

The system validates:

* Same key + same payload → return stored response
* Same key + different payload → reject request

This prevents duplicate transfers while supporting safe retries.

---

## Concurrency

Concurrency is handled using:

* Pessimistic row-level locking
* Deterministic wallet lock ordering
* Retry mechanism

This prevents:

* Double spending
* Lost updates
* Most deadlock scenarios

---

## Ledger Consistency

The system follows a double-entry ledger model.

Every successful transfer generates:

* One DEBIT entry
* One CREDIT entry

Invariant:

```text
Total Debits = Total Credits
```

This guarantees complete auditability.

---

# Architecture

```text
Controller
    ↓
TransferService
    ↓
TransferServiceImpl
    ↓
TransferDomainService
    ↓
Repositories
    ↓
PostgreSQL
```

---

# Design Patterns Used

## Repository Pattern

Repositories:

* WalletRepository
* TransferRepository
* LedgerEntryRepository
* IdempotencyRecordRepository

Purpose:

* Persistence abstraction
* Cleaner service layer
* Easier testing

---

## Factory Pattern

Class:

`LedgerEntryFactory`

Purpose:

* Create debit ledger entries
* Create credit ledger entries

---

## Domain Service Pattern

Class:

`TransferDomainService`

Purpose:

* Balance validation
* Wallet debit
* Wallet credit
* Ledger creation
* Transfer state transitions

---

## DTO Pattern

Classes:

* TransferRequest
* TransferResponse
* ErrorResponse

Purpose:

* Separate API contracts from entities

---

## State Transition Pattern

Implemented in `Transfer`.

Methods:

* markProcessed()
* markFailed()

Prevents invalid status transitions.

---

# Database Design

## wallets

Stores wallet balances.

| Column     | Description                       |
| ---------- | --------------------------------- |
| id         | Wallet identifier                 |
| balance    | Current balance                   |
| version    | Future optimistic locking support |
| created_at | Creation timestamp                |
| updated_at | Last update timestamp             |

---

## transfers

Stores business transaction information.

| Column         | Description           |
| -------------- | --------------------- |
| id             | Transfer identifier   |
| from_wallet_id | Source wallet         |
| to_wallet_id   | Destination wallet    |
| amount         | Transfer amount       |
| status         | Transfer state        |
| failure_reason | Failure details       |
| created_at     | Creation timestamp    |
| updated_at     | Last update timestamp |

---

## ledger_entries

Stores immutable accounting records.

| Column      | Description        |
| ----------- | ------------------ |
| id          | Ledger identifier  |
| transfer_id | Related transfer   |
| wallet_id   | Related wallet     |
| entry_type  | DEBIT or CREDIT    |
| amount      | Ledger amount      |
| created_at  | Creation timestamp |

---

## idempotency_records

Stores retry information.

| Column           | Description         |
| ---------------- | ------------------- |
| id               | Record identifier   |
| idempotency_key  | Unique request key  |
| request_hash     | Request fingerprint |
| transfer_id      | Related transfer    |
| response_payload | Original response   |
| status           | Processing status   |
| created_at       | Audit timestamp     |

---

# Database Relationships

```text
Wallet
  │
  ├── Source Transfer
  │
  └── Destination Transfer

Transfer
  │
  ├── Ledger Entries
  │
  └── Idempotency Record
```

---

# Class Responsibilities

## TransferController

* HTTP request handling
* Validation
* Service delegation

## TransferServiceImpl

* Transaction orchestration
* Idempotency coordination
* Lock acquisition
* Repository coordination

## TransferDomainService

* Business rules
* Balance validation
* Debit/Credit execution
* Ledger generation

## LedgerEntryFactory

* Debit entry creation
* Credit entry creation

## IdempotencyService

* Idempotency validation
* Response replay
* Request hash verification

## HashingService

* SHA-256 request hashing

---

# SOLID Principles Applied

## Single Responsibility Principle

Each class owns a single concern.

Examples:

* LedgerEntryFactory → Ledger creation
* HashingService → Request hashing
* TransferController → HTTP concerns

---

## Open Closed Principle

Business behavior can be extended without modifying existing components.

---

## Dependency Inversion Principle

Controllers depend on interfaces.

Services depend on abstractions rather than implementations.

---

# Future Improvements

Potential enhancements:

* Outbox Pattern
* Kafka integration
* Saga orchestration
* Transfer history APIs
* Wallet statement APIs
* Monitoring and metrics
* Distributed idempotency

For the scope of this assignment, a modular monolithic architecture provides the best balance between correctness, maintainability, and delivery speed.
