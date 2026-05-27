# Automated Scripts

Quick scripts to setup, run, and test the wallet transfer service.

## Quick Start (3 Commands)

```bash
# 1. Setup database and build application
./scripts/setup.sh

# 2. Start server (in one terminal)
./scripts/run.sh

# 3. Run automated tests (in another terminal)
./scripts/test.sh
```

That's it! The test script will run all 8 production tests automatically.

---

## Script Details

### setup.sh
**Purpose:** One-time setup
**What it does:**
- Creates `config.json` if not exists
- Checks PostgreSQL connection
- Creates database if not exists
- Installs Go dependencies
- Builds the application

**Usage:**
```bash
./scripts/setup.sh
```

**Prerequisites:**
- PostgreSQL running locally
- Go 1.20+ installed

---

### run.sh
**Purpose:** Start the server with test data
**What it does:**
- Builds application (if needed)
- Seeds test wallets (wallet_1: $1000, wallet_2: $500)
- Runs migrations automatically
- Starts HTTP server on port 8080

**Usage:**
```bash
./scripts/run.sh
```

**Test wallets created:**
- `wallet_1`: $1000.00
- `wallet_2`: $500.00

Keep this running while testing.

---

### test.sh
**Purpose:** Automated end-to-end testing
**What it does:**
- Runs 8 comprehensive tests
- Verifies all production features
- Checks database state
- Prints colored pass/fail results

**Usage:**
```bash
# Server must be running first (./scripts/run.sh)
./scripts/test.sh
```

**Tests performed:**
1. Health check
2. Successful transfer
3. Idempotency (duplicate request)
4. Request hash validation
5. Insufficient balance (NOT cached)
6. Wallet not found (NOT cached)
7. Double-entry ledger integrity
8. Transfer count verification

**Expected output:** All green checkmarks ✓

---

### seed_data.sql
**Purpose:** Test data for wallets
**What it does:**
- Creates test wallets if they don't exist
- Resets balances to defaults
- Can be customized for different test scenarios

**Usage:**
```bash
# Manually load (if needed)
psql -U postgres -d wallet_transfer -f scripts/seed_data.sql
```

**Customize:** Edit this file to add more wallets or change initial balances.

---

## Customization

### Change Database Credentials
Edit `config.json`:
```json
{
  "database": {
    "host": "localhost",
    "port": "5432",
    "user": "your_user",
    "password": "your_password",
    "dbname": "wallet_transfer",
    "sslmode": "disable"
  }
}
```

### Add More Test Wallets
Edit `scripts/seed_data.sql`:
```sql
INSERT INTO wallets (id, balance, created_at, updated_at)
VALUES
  ('wallet_1', 1000.00, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  ('wallet_2', 500.00, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  ('wallet_3', 2000.00, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);  -- Add more here
```

### Custom API Tests
Edit `scripts/test.sh` to add your own curl commands.

---

## Troubleshooting

### "PostgreSQL is not running"
```bash
# macOS (Homebrew)
brew services start postgresql

# Linux
sudo systemctl start postgresql
```

### "Server is not running"
Make sure `./scripts/run.sh` is running in another terminal.

### "Permission denied"
```bash
chmod +x scripts/*.sh
```

### Clean Database and Restart
```bash
# Drop database
psql -U postgres -c "DROP DATABASE wallet_transfer;"

# Re-run setup
./scripts/setup.sh
./scripts/run.sh
```

---

## Manual Testing

For manual curl commands, see: [../TESTING.md](../TESTING.md)

For detailed documentation, see: [../README.md](../README.md)
