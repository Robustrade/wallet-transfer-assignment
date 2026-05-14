# Wallet Transfer Service - Setup and Testing Guide

This document provides comprehensive instructions on how to run, test, and interact with the Wallet Transfer Service.

## 1. Prerequisites

Ensure you have the following installed:

- [Docker](https://docs.docker.com/get-docker/) and [Docker Compose](https://docs.docker.com/compose/install/)
- [Go 1.23+](https://go.dev/dl/) (optional, for running tests locally)
- `curl` (for API testing)

---

## 2. Running the Service

The easiest way to start the entire stack (Go Application + PostgreSQL) is using the provided `run.sh` script.

### Steps:

1. Open your terminal at the project root.
2. Make the script executable:
   ```bash
   chmod +x run.sh
   ```
3. Start the services:
   ```bash
   ./run.sh
   ```

This script will:

- Stop any conflicting containers.
- Build the Go application using the `Dockerfile`.
- Start PostgreSQL and the Application via `docker-compose.yml`.
- Follow the logs to ensure the database migrations complete successfully.

---

## 3. Running Test Cases

The project includes a suite of unit tests and integration tests to ensure reliability.

### Automated Testing Script

Run the `test.sh` script to execute both unit and integration tests in an isolated environment:

```bash
chmod +x test.sh
./test.sh
```

### Manual Testing Commands

**Unit Tests (No DB required):**

```bash
go test ./internal/...
```

**Integration Tests (Requires DB):**
Ensure the database is running (`docker compose up db -d`), then run:

```bash
DB_HOST=localhost DB_PORT=5432 DB_USER=admin DB_PASSWORD=admin123 DB_NAME=my_db go test ./tests/... -v
```

---

## 4. API Testing (curl)

Once the server is running on `http://localhost:8080`, use these commands to verify functionality.

### A. Health Check

```bash
curl -i http://localhost:8080/health
```

### B. Create Wallets

Create two wallets to perform a transfer. Note the `wallet_id` returned in the response.

**Wallet 1:**

```bash
curl -i -X POST http://localhost:8080/wallets \
     -H "Content-Type: application/json" \
     -d '{"initialBalance": 1000}'
```

**Wallet 2:**

```bash
curl -i -X POST http://localhost:8080/wallets \
     -H "Content-Type: application/json" \
     -d '{"initialBalance": 0}'
```

### C. Execute a Transfer

Replace the UUIDs below with the ones generated in the previous step.

```bash
curl -i -X POST http://localhost:8080/transfers \
     -H "Content-Type: application/json" \
     -d '{
       "idempotencyKey": "unique-request-id-001",
       "fromWalletId": "REPLACE_WITH_W1_UUID",
       "toWalletId": "REPLACE_WITH_W2_UUID",
       "amount": 250
     }'
```

_Note: Sending this exact request again with the same `idempotencyKey` will return the cached result without creating a duplicate transfer._

### D. Check Wallet Balance

```bash
curl -i http://localhost:8080/wallets/balance/REPLACE_WITH_W1_UUID
```

---

## 5. Architectural Implementation Details

- **Concurrency**: Uses `SELECT ... FOR UPDATE` via GORM's `clause.Locking` to prevent double-spending.
- **Atomicity**: All operations (Transfer record, Ledger entries, Balance updates) are wrapped in a single database transaction.
- **Idempotency**: Managed via a dedicated `idempotency_records` table with a unique constraint on the key, storing the serialized JSON response for replays.
- **Persistence**: Data is stored in PostgreSQL with GORM handling the schema migrations automatically on startup.
