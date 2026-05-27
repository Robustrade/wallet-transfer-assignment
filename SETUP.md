# Wallet Transfer Service - Setup & Usage Guide

## Overview

A production-grade wallet transfer service implementing:
- **Idempotency** - Atomic duplicate prevention using database constraints
- **Concurrency safety** - Deterministic locking to prevent race conditions and deadlocks
- **Double-entry ledger** - Immutable audit trail for all transfers
- **Transaction safety** - All operations in a single ACID transaction

---

## Prerequisites

- **Go 1.20+** installed
- **PostgreSQL 12+** running
- **Git** for version control

---

## Quick Start

### 1. Configure Database

Copy the example config and update with your database credentials:

```bash
cp config.example.json config.json
```

Edit `config.json`:
```json
{
  "database": {
    "host": "localhost",
    "port": "5432",
    "user": "postgres",
    "password": "your_password",
    "dbname": "wallet_transfer",
    "sslmode": "disable"
  },
  "server": {
    "port": "8080"
  },
  "environment": "development"
}
```

### 2. Create Database

```bash
# Connect to PostgreSQL
psql -U postgres

# Create database
CREATE DATABASE wallet_transfer;
\q
```

### 3. Install Dependencies

```bash
go mod download
```

### 4. Build & Run

```bash
# Build
go build -o bin/server ./cmd/server

# Run
./bin/server
```

The server will:
1. Connect to the database
2. Run migrations automatically
3. Start HTTP server on port 8080

---

## API Usage

### Create Transfer

**Endpoint:** `POST /transfers`

**Request:**
```json
{
  "idempotencyKey": "unique-key-123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100.50
}
```

**Success Response (200 OK):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100.50,
  "status": "PROCESSED",
  "createdAt": "2026-05-25T00:00:00Z"
}
```

### Health Check

**Endpoint:** `GET /health`

---

## Testing

### Create Test Wallets

```bash
psql -U postgres -d wallet_transfer
```

```sql
INSERT INTO wallets (id, balance, created_at, updated_at)
VALUES
  ('wallet_1', 1000.00, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  ('wallet_2', 500.00, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
```

### Test Transfer

```bash
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-001",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 100.50
  }'
```

### Test Idempotency

```bash
# Same request returns same response (cached)
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-001",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 100.50
  }'
```

### Test Request Hash Validation

```bash
# Same key, different amount = 409 Conflict
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-001",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 200.00
  }'
```

### Test Validation Errors (NOT Cached)

```bash
# Insufficient balance - NOT cached, can retry after deposit
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-002",
    "fromWalletId": "wallet_2",
    "toWalletId": "wallet_1",
    "amount": 999999.00
  }'

# Retry with same key - still fails (not cached)
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-002",
    "fromWalletId": "wallet_2",
    "toWalletId": "wallet_1",
    "amount": 999999.00
  }'
```

---

## Run Tests

```bash
go test ./... -v
```

---

## Troubleshooting

### "Failed to connect to database"
- Ensure PostgreSQL is running
- Check credentials in `config.json`
- Verify database exists

### "Wallet not found"
- Insert test wallets using SQL above

### Build errors
```bash
go clean -cache
go mod download
go build -o bin/server ./cmd/server
```
