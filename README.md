# Wallet Transfer API

A backend wallet management and money transfer system built using **Go (Golang)** and **PostgreSQL**. The application supports wallet creation, secure wallet-to-wallet transfers, atomic database transactions, idempotency, and double-entry ledger entries.

## Table of Contents

- [Overview](#overview)
- [Technology Stack](#technology-stack)
- [Features](#features)
- [Project Structure](#project-structure)
- [Architecture](#architecture)
- [Database Design](#database-design)
- [API Documentation](#api-documentation)
- [Getting Started](#getting-started)
- [Environment Configuration](#environment-configuration)
- [Running the Application](#running-the-application)
- [Running Tests](#running-tests)
- [Idempotency Strategy](#idempotency-strategy)
- [Concurrency and Transaction Strategy](#concurrency-and-transaction-strategy)
- [Ledger Management](#ledger-management)
- [Error Handling](#error-handling)

## Overview

The Wallet Transfer API allows users to create wallets and transfer money between wallets securely.

The application is designed to maintain financial consistency using PostgreSQL transactions, row-level locking, idempotency keys, and double-entry ledger records.

All transfer-related database changes are committed together. If an operation fails before commit, the transaction is rolled back.

## Technology Stack

| Technology | Purpose |
|---|---|
| Go (Golang) | Backend development |
| PostgreSQL | Relational database |
| pgx / pgxpool | PostgreSQL connectivity |
| REST API | Client-server communication |
| Postman | API testing |
| Git / GitHub | Version control |

## Features

- Create a new wallet.
- Transfer money between wallets.
- Validate transfer requests.
- Prevent transfers between the same wallet.
- Check sufficient wallet balance before debit.
- Perform atomic database transactions.
- Use row-level locking to protect concurrent transfers.
- Prevent duplicate transfers using idempotency keys.
- Maintain DEBIT and CREDIT ledger entries.
- Store transfer history in PostgreSQL.
- Handle database errors and invalid requests.

## Project Structure

```text
wallet-transfer-assignment/
│
├── cmd/
│   └── api/
│       └── main.go
│
├── internal/
│   ├── config/
│   │   └── database.go
│   │
│   ├── domain/
│   │   ├── ledger.go
│   │   ├── transfer.go
│   │   └── wallet.go
│   │
│   ├── handler/
│   │   ├── transfer_handler.go
│   │   └── wallet_handler.go
│   │
│   ├── repository/
│   │   ├── transfer_repository.go
│   │   └── wallet_repository.go
│   │
│   └── service/
│       ├── transfer_service.go
│       ├── validation.go
│       └── validation_test.go
│
├── migrations/
├── tests/
├── go.mod
└── go.sum
```

## Architecture

The application follows a layered architecture:

```text
Client / Postman
       |
       v
HTTP Handler Layer
       |
       v
Service Layer
       |
       v
Repository Layer
       |
       v
PostgreSQL Database
```

**Handler Layer:** Receives HTTP requests, parses JSON, and returns HTTP responses.

**Service Layer:** Handles business logic and validates transfer requests.

**Repository Layer:** Executes database queries and manages transactions.

**Domain Layer:** Defines core application models such as Wallet, Transfer, and Ledger.

**Config Layer:** Manages PostgreSQL database connectivity.

## Database Design

The application uses PostgreSQL with the following tables:

### 1. wallets

Stores wallet information.

- `id` – UUID primary key.
- `balance` – Wallet balance using NUMERIC.
- `created_at` – Wallet creation timestamp.
- `updated_at` – Last update timestamp.

### 2. transfers

Stores wallet transfer records.

- `id` – UUID primary key.
- `from_wallet_id` – Sender wallet foreign key.
- `to_wallet_id` – Receiver wallet foreign key.
- `amount` – Transfer amount.
- `status` – Transfer status.
- `created_at` – Transfer creation timestamp.

### 3. ledger_entries

Stores debit and credit records for each transfer.

- `id` – UUID primary key.
- `wallet_id` – Wallet foreign key.
- `transfer_id` – Transfer foreign key.
- `entry_type` – DEBIT or CREDIT.
- `amount` – Ledger amount.
- `created_at` – Entry creation timestamp.

### 4. idempotency_records

Stores idempotency information to prevent duplicate processing.

- `idempotency_key` – Unique request key.
- `request_hash` – Hash of the transfer request.
- `transfer_id` – Associated transfer reference.

## API Documentation

Base URL:

```text
http://localhost:8081
```

### 1. Health Check

**GET** `/health`

Checks whether the API is running.

### 2. Create Wallet

**POST** `/wallets`

Request Body:

```json
{
  "balance": 500
}
```

Example successful response:

```json
{
  "id": "WALLET_UUID",
  "balance": 500,
  "currency": "INR",
  "createdAt": "TIMESTAMP"
}
```

The wallet creation balance is provided in INR.

### 3. Transfer Money

**POST** `/transfers`

Required Header:

```text
Content-Type: application/json
Idempotency-Key: transfer-001
```

Request Body:

```json
{
  "fromWalletId": "SENDER_WALLET_UUID",
  "toWalletId": "RECEIVER_WALLET_UUID",
  "amount": 10000
}
```

**Amount format:** Transfer amount is provided in paise. For example, `10000` represents ₹100.

Example successful response:

```json
{
  "id": "TRANSFER_UUID",
  "fromWalletId": "SENDER_WALLET_UUID",
  "toWalletId": "RECEIVER_WALLET_UUID",
  "amount": 10000,
  "status": "PROCESSED",
  "createdAt": "TIMESTAMP"
}
```

## Getting Started

### Prerequisites

Install the following:

- Go
- PostgreSQL
- Git
- Postman (for API testing)

### 1. Clone the Repository

```bash
git clone https://github.com/skc1234/wallet-transfer-assignment.git
```

### 2. Navigate to the Project

```bash
cd wallet-transfer-assignment
```

### 3. Install Dependencies

```bash
go mod tidy
```

### 4. Create PostgreSQL Database

Create a database named:

```sql
CREATE DATABASE wallet_transfer;
```

Create the required database tables using the SQL schema provided in the project's migration scripts.

## Environment Configuration

Configure the PostgreSQL connection using the `DATABASE_URL` environment variable.

For Windows PowerShell:

```powershell
$env:DATABASE_URL="postgres://USERNAME:YOUR_PASSWORD@localhost:5432/wallet_transfer"
```

Replace the username and password with your local PostgreSQL credentials.

Do not commit actual database passwords or secrets to GitHub.

## Running the Application

Start the API:

```bash
go run ./cmd/api
```

The application starts on:

```text
http://localhost:8081
```

Check the health endpoint:

```text
GET http://localhost:8081/health
```

## Running Tests

Run all Go tests:

```bash
go test ./...
```

Run tests with verbose output:

```bash
go test -v ./...
```

## Idempotency Strategy

The transfer API uses an idempotency key to prevent duplicate transfer processing.

The implementation:

1. Receives an idempotency key from the request header.
2. Generates a SHA-256 hash from the request payload.
3. Stores the key and request hash in PostgreSQL.
4. Checks whether the key has already been used.
5. Returns the previous transfer for an identical request.
6. Rejects reuse of the same key with a different request.

This helps prevent duplicate financial operations when clients retry requests.

## Concurrency and Transaction Strategy

PostgreSQL transactions are used to maintain consistency during wallet transfers.

The implementation:

1. Starts a database transaction.
2. Locks sender and receiver wallet rows using `SELECT FOR UPDATE`.
3. Acquires locks in a consistent order.
4. Checks the sender's available balance.
5. Debits the sender wallet.
6. Credits the receiver wallet.
7. Creates the transfer record.
8. Creates DEBIT and CREDIT ledger entries.
9. Updates the idempotency record.
10. Commits all changes together.

If any database operation fails, the transaction is rolled back.

## Ledger Management

Each successful transfer creates two ledger entries:

- **DEBIT:** Records the amount deducted from the sender's wallet.
- **CREDIT:** Records the amount added to the receiver's wallet.

Both entries reference the same transfer ID.

This provides an auditable record of wallet movements.

## Error Handling

The API handles common failure scenarios, including:

- Invalid request body.
- Invalid transfer amount.
- Missing wallet IDs.
- Same sender and receiver wallet.
- Insufficient wallet balance.
- Wallet not found.
- Missing idempotency key.
- Reuse of an idempotency key with a different request.
- Database transaction failures.

HTTP error responses are returned when requests cannot be processed.

---

## Author

**Saurav Kumar Choudhary**

GitHub: [skc1234](https://github.com/skc1234)

Repository: [wallet-transfer-assignment](https://github.com/skc1234/wallet-transfer-assignment)