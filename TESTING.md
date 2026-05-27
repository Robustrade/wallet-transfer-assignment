# Testing Guide for Reviewers

This guide provides step-by-step instructions to test the wallet transfer service from scratch.

---

## Prerequisites

- **Go 1.20+** installed (`go version`)
- **PostgreSQL 12+** running (`psql --version`)
- **curl** for API testing

---

## Step 1: Database Setup

### 1.1 Start PostgreSQL
```bash
# Ensure PostgreSQL is running
# macOS (Homebrew): brew services start postgresql
# Linux: sudo systemctl start postgresql
```

### 1.2 Create Database
```bash
# Connect to PostgreSQL
psql -U postgres

# Create database
CREATE DATABASE wallet_transfer;

# Exit psql
\q
```

### 1.3 Configure Application
```bash
# Copy example config
cp config.example.json config.json

# Edit config.json with your database credentials
# Default should work if PostgreSQL is running locally with user 'postgres'
```

Example `config.json`:
```json
{
  "database": {
    "host": "localhost",
    "port": "5432",
    "user": "postgres",
    "password": "",
    "dbname": "wallet_transfer",
    "sslmode": "disable"
  },
  "server": {
    "port": "8080"
  },
  "environment": "development"
}
```

---

## Step 2: Build and Run

### 2.1 Install Dependencies
```bash
go mod download
```

### 2.2 Build
```bash
go build -o bin/server ./cmd/server
```

### 2.3 Run Server
```bash
./bin/server
```

**Expected output:**
```
Successfully connected to database
Running database migrations...
Database migrations completed
Starting server on :8080
```

The server will:
1. Connect to PostgreSQL
2. Run migrations automatically (creates all tables)
3. Start HTTP server on port 8080

---

## Step 3: Create Test Data

Open a new terminal and create test wallets:

```bash
psql -U postgres -d wallet_transfer
```

```sql
INSERT INTO wallets (id, balance, created_at, updated_at)
VALUES
  ('wallet_1', 1000.00, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  ('wallet_2', 500.00, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

-- Verify wallets
SELECT * FROM wallets;
```

Expected output:
```
    id     | balance  |         created_at         |         updated_at
-----------+----------+----------------------------+----------------------------
 wallet_1  | 1000.00  | 2026-05-27 10:00:00.000000 | 2026-05-27 10:00:00.000000
 wallet_2  |  500.00  | 2026-05-27 10:00:00.000000 | 2026-05-27 10:00:00.000000
```

---

## Step 4: Test Core Functionality

### Test 1: Successful Transfer

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

**Expected Response (200 OK):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100.5,
  "status": "PROCESSED",
  "createdAt": "2026-05-27T10:00:00Z"
}
```

**Verify in Database:**
```sql
-- Check balances updated
SELECT id, balance FROM wallets;
-- wallet_1: 899.50, wallet_2: 600.50

-- Check transfer created
SELECT * FROM transfers;

-- Check ledger entries (double-entry)
SELECT * FROM ledger_entries ORDER BY created_at;
-- Should show 2 entries: 1 DEBIT, 1 CREDIT
```

---

## Step 5: Test Idempotency

### Test 2: Duplicate Request (Same Payload)

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

**Expected Response (200 OK - SAME as Test 1):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100.5,
  "status": "PROCESSED",
  "createdAt": "2026-05-27T10:00:00Z"
}
```

**Verify Balance NOT Changed:**
```sql
SELECT id, balance FROM wallets;
-- wallet_1: 899.50 (unchanged)
-- wallet_2: 600.50 (unchanged)
```

**Key Point:** Duplicate request returns cached response, balance NOT debited twice.

---

## Step 6: Test Request Hash Validation

### Test 3: Same Key, Different Payload

```bash
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-001",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 200.00
  }'
```

**Expected Response (409 Conflict):**
```json
{
  "error": "idempotency key reused with different payload"
}
```

**Key Point:** Same idempotency key with different amount is rejected.

---

## Step 7: Test Validation Errors NOT Cached

### Test 4: Insufficient Balance

```bash
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-002",
    "fromWalletId": "wallet_2",
    "toWalletId": "wallet_1",
    "amount": 999999.00
  }'
```

**Expected Response (400 Bad Request):**
```json
{
  "error": "insufficient balance"
}
```

### Test 5: Retry Same Request (Should FAIL Again, NOT Cached)

```bash
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-002",
    "fromWalletId": "wallet_2",
    "toWalletId": "wallet_1",
    "amount": 999999.00
  }'
```

**Expected Response (400 Bad Request - SAME ERROR, NOT CACHED):**
```json
{
  "error": "insufficient balance"
}
```

**Key Point:** Validation errors are NOT cached. Client can retry after fixing the issue (e.g., depositing funds).

---

## Step 8: Test Wallet Not Found (Validation Error)

### Test 6: Non-existent Wallet

```bash
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-003",
    "fromWalletId": "wallet_999",
    "toWalletId": "wallet_2",
    "amount": 50.00
  }'
```

**Expected Response (400 Bad Request):**
```json
{
  "error": "wallet not found: wallet_999"
}
```

### Test 7: Create Wallet and Retry

```sql
-- Create the missing wallet
INSERT INTO wallets (id, balance, created_at, updated_at)
VALUES ('wallet_999', 1000.00, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
```

```bash
# Retry with SAME idempotency key
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-003",
    "fromWalletId": "wallet_999",
    "toWalletId": "wallet_2",
    "amount": 50.00
  }'
```

**Expected Response (200 OK - Now succeeds):**
```json
{
  "id": "...",
  "fromWalletId": "wallet_999",
  "toWalletId": "wallet_2",
  "amount": 50,
  "status": "PROCESSED",
  "createdAt": "..."
}
```

**Key Point:** After fixing the validation error (creating wallet), the same idempotency key works because validation errors are NOT cached.

---

## Step 9: Test Health Endpoint

```bash
curl http://localhost:8080/health
```

**Expected Response (200 OK):**
```json
{"status":"ok"}
```

---

## Step 10: Verify Database Integrity

### Check Double-Entry Ledger Balance

```sql
-- Total debits should equal total credits
SELECT
  SUM(CASE WHEN entry_type = 'CREDIT' THEN amount ELSE 0 END) as total_credits,
  SUM(CASE WHEN entry_type = 'DEBIT' THEN amount ELSE 0 END) as total_debits,
  SUM(CASE WHEN entry_type = 'CREDIT' THEN amount ELSE -amount END) as net_balance
FROM ledger_entries;
```

**Expected:** `net_balance` should be `0` (double-entry accounting).

### Check Idempotency Records

```sql
SELECT idempotency_key, status, transfer_id FROM idempotency_records;
```

**Expected:**
- `test-001`: status = `COMPLETED`, has transfer_id
- `test-002`: NOT present (validation errors not cached)
- `test-003`: status = `COMPLETED`, has transfer_id

---

## Step 11: Run Unit Tests

```bash
go test ./... -v
```

**Expected:** All tests pass (9 test suites).

---

## Summary of What Was Tested

| Feature | Test | Expected Behavior |
|---------|------|-------------------|
| **Basic Transfer** | Test 1 | Creates transfer, updates balances, ledger entries |
| **Idempotency** | Test 2 | Same request returns cached response, no duplicate charge |
| **Request Hash Validation** | Test 3 | Same key + different payload = 409 Conflict |
| **Validation Errors NOT Cached** | Test 4-5 | Insufficient balance error NOT cached, can retry |
| **Wallet Not Found NOT Cached** | Test 6-7 | After creating wallet, same key succeeds |
| **Double-Entry Accounting** | Step 10 | Ledger balances to zero |
| **Atomic Transactions** | All tests | No partial transfers possible |
| **Health Check** | Test 9 | Service is running |

---

## Production Features Demonstrated

1. **Atomic Idempotency** - Database UNIQUE constraint prevents race conditions
2. **Request Hash Validation** - Detects key reuse with different payload
3. **Smart Caching** - Validation errors NOT cached, allows retry after fixing issues
4. **PROCESSING Timeout** - 30-second timeout handles crashed requests (code review)
5. **Concurrency Safety** - Deterministic locking prevents deadlocks (code review)
6. **Double-Entry Ledger** - Immutable audit trail
7. **Connection Pool** - MaxOpenConns=25 for scalability (code review)

---

## Troubleshooting

### "Failed to connect to database"
- Ensure PostgreSQL is running
- Check credentials in `config.json`
- Verify database exists: `psql -U postgres -l`

### "Wallet not found"
- Insert test wallets (Step 3)

### Port 8080 already in use
- Change port in `config.json` under `server.port`
- Or kill process: `lsof -ti:8080 | xargs kill`

---

## Code Review Points

For reviewers examining the code:

1. **transfer_service.go:58-97** - Atomic idempotency claim with INSERT ON CONFLICT
2. **transfer_service.go:99-101** - Deterministic lock ordering (prevents deadlocks)
3. **transfer_service.go:118-122** - Validation errors NOT cached (allows retry)
4. **transfer_service.go:207-212** - PROCESSING timeout handling
5. **cmd/server/main.go:33-35** - Connection pool configuration
6. **docs/schema.md** - Complete schema design documentation

---

## Next Steps

After testing, review:
- `README.md` - Project overview and features
- `docs/schema.md` - Database design and concurrency strategy
- `internal/services/transfer_service.go` - Core business logic
- `SETUP.md` - Additional setup details
