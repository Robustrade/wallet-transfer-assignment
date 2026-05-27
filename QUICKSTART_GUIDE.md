# Quick Start Guide for Reviewers

This guide is for interviewers/reviewers who want to quickly test the wallet transfer service.

---

## TL;DR - Single Command

```bash
./quickstart.sh
```

**Done!** This will:
1. Setup database and dependencies
2. Build and start the server
3. Run all 8 automated tests
4. Show you the results

**Time:** ~2 minutes

---

## What Gets Tested Automatically

The `quickstart.sh` script runs 8 comprehensive tests:

| # | Test | What It Validates |
|---|------|-------------------|
| 1 | Health Check | Server is running |
| 2 | Successful Transfer | Money moves between wallets |
| 3 | Idempotency | Duplicate requests return cached response |
| 4 | Request Hash Validation | Same key + different payload = rejected |
| 5 | Insufficient Balance | Validation errors NOT cached |
| 6 | Wallet Not Found | Can retry after fixing validation error |
| 7 | Ledger Integrity | Double-entry accounting balances to zero |
| 8 | Record Tracking | Transfers match idempotency records |

**All tests run automatically with a single command.**

---

## Expected Output

```
==========================================
Wallet Transfer Service - Quick Start
==========================================

[1/3] Running setup...
✓ Created config.json
✓ PostgreSQL is running
✓ Database created
✓ Dependencies installed
✓ Application built successfully

[2/3] Starting server in background...
✓ Server started (PID: 12345)

Waiting for server to be ready...
✓ Server is ready!

[3/3] Running automated tests...

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
TEST 1: Health Check
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ Health check passed

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
TEST 2: Successful Transfer
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ Transfer successful
  Transfer ID: 550e8400-e29b-41d4-a716-446655440000
✓ Balances updated correctly
  wallet_1: $1000 → $899.50
  wallet_2: $500 → $600.50

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
TEST 3: Idempotency - Duplicate Request
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ Returned cached response with same transfer ID
✓ Balances unchanged (no duplicate charge)

... (tests 4-8) ...

==========================================
All Tests Passed! ✓
==========================================
```

---

## If You Want to Test Manually

After running `./quickstart.sh`, the server stays running. You can test manually:

```bash
# Transfer money
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "manual-test-001",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 50.00
  }'

# Check database
psql -U postgres -d wallet_transfer -c "SELECT * FROM wallets;"
```

---

## Customization Options

### Change Test Wallets
Edit `scripts/seed_data.sql`:
```sql
INSERT INTO wallets (id, balance, created_at, updated_at)
VALUES
  ('wallet_1', 1000.00, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  ('wallet_2', 500.00, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  ('wallet_3', 5000.00, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);  -- Add more
```

Then restart:
```bash
./scripts/run.sh
```

### Add Custom Tests
Edit `scripts/test.sh` and add your own curl commands following the existing pattern.

### Use Different Database
Edit `config.json`:
```json
{
  "database": {
    "host": "localhost",
    "port": "5432",
    "user": "your_user",
    "password": "your_password",
    "dbname": "wallet_transfer"
  }
}
```

---

## File Structure

```
wallet-transfer-assignment/
├── quickstart.sh              # One-command setup + test
├── scripts/
│   ├── README.md              # Script documentation
│   ├── setup.sh               # Database setup + build
│   ├── run.sh                 # Start server
│   ├── test.sh                # Automated tests
│   └── seed_data.sql          # Test wallet data
├── README.md                  # Project overview
├── TESTING.md                 # Manual testing guide
└── docs/
    └── schema.md              # Database design docs
```

---

## Stop the Server

```bash
# Find the PID (shown in quickstart output)
kill <PID>

# Or find it manually
lsof -ti:8080 | xargs kill
```

---

## Troubleshooting

### "PostgreSQL is not running"
```bash
# macOS
brew services start postgresql

# Linux
sudo systemctl start postgresql
```

### "Port 8080 already in use"
```bash
# Kill existing process
lsof -ti:8080 | xargs kill

# Or change port in config.json
```

### Start Fresh
```bash
# Drop database and start over
psql -U postgres -c "DROP DATABASE wallet_transfer;"
./quickstart.sh
```

---

## What to Review

After running tests, review the code:

### Core Business Logic
- `internal/services/transfer_service.go` - Idempotency, transactions, locking
- `internal/repositories/*` - Data access layer
- `internal/handlers/*` - HTTP layer

### Database Design
- `docs/schema.md` - Complete schema documentation
- `migrations/` - Database migrations

### Production Features
Look for:
- **Line 58-97** in `transfer_service.go` - Atomic idempotency claim
- **Line 99-101** - Deterministic lock ordering (deadlock prevention)
- **Line 118-122** - Validation errors NOT cached
- **Line 207-212** - PROCESSING timeout handling
- **Line 33-35** in `cmd/server/main.go` - Connection pool config

---

## Documentation

- **README.md** - Project overview and features
- **TESTING.md** - Manual testing guide with curl commands
- **docs/schema.md** - Database design and concurrency strategy
- **scripts/README.md** - Script documentation and customization
- **SETUP.md** - Detailed setup instructions

---

## Questions?

All production features are documented with rationale:
- See **"Production Considerations"** section in README.md
- See **"Known Limitations & Trade-offs"** section in README.md

The implementation is production-grade within the assignment scope, with honest documentation of trade-offs.
