# Database Migrations Guide

## Overview

This directory contains versioned SQL migrations for the wallet transfer platform. Migrations are applied in order, enabling schema evolution with full auditability.

### Files

| File                                      | Purpose | Required       | Status                                     |
|-------------------------------------------|---------|----------------|--------------------------------------------|
| `V000__init_wallets_and_transfers.sql`    | Rolls back the initial schema creation.Tables are dropped in reverse foreign-key dependency order. | ❌ **Optional** | **PRE RUN Only if Schema Already Exists ** |
| `V001__init_wallets_and_transfers.sql`    | Core wallet and transfer tables | ✅ **Required** | **APPLY FIRST**                            |
| `V002__init_ledger_and_idempotency.sql`   | Ledger entries and idempotency | ✅ **Required** | **APPLY SECOND**                           |
| `V003__audit_logging_and_scalability.sql` | Optional audit logging and analytics | ❌ Optional     | Advanced features                          |

---

## Quick Start

### Option 1: Using psql (Manual)

```bash
# Connect to PostgreSQL
psql -h localhost -U postgres -d wallet_transfer

# Apply migrations in order
\i migrations/V001__init_wallets_and_transfers.sql
\i migrations/V002__init_ledger_and_idempotency.sql

# Verify schema
\dt
\dv

# View indexes
\di
```

### Option 2: Using a Migration Tool (Recommended for Production)

#### Flyway (Java-based, cross-platform)

```bash
# Install Flyway
# https://flywaydb.org/documentation/usage/commandline/

# Configure flyway.conf
flyway_locations=filesystem:/path/to/migrations
flyway_url=jdbc:postgresql://localhost:5432/wallet_transfer
flyway_user=postgres
flyway_password=yourpassword

# Run migrations
flyway migrate

# Check status
flyway info
```

#### Liquibase (XML/YAML-based)

```bash
# Create changeLog.xml referencing SQL files
# Run: liquibase update

# Liquibase provides more complex dependency management
```

#### golang-migrate (Go-based)

```bash
# Install
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Run migrations
migrate -path ./migrations -database "postgres://user:password@localhost:5432/wallet_transfer?sslmode=disable" up

# Undo migration
migrate -path ./migrations -database "..." down 1
```

#### Application-Embedded (Recommended for this project)

```go
// In your Go application startup:

import "github.com/golang-migrate/migrate/v4"

func initDatabase(dbURL string) error {
    m, err := migrate.New(
        "file://./migrations",
        dbURL,
    )
    if err != nil {
        return err
    }

    err = m.Up()
    if err != nil && err != migrate.ErrNoChange {
        return err
    }
    return nil
}

// Call during application startup
initDatabase("postgres://...")
```

---

## Migration Details

### V001: Wallets and Transfers

**Tables Created:**
- `wallets`: Wallet state with balance and version
- `transfers`: Transfer requests with idempotency key
- Enums: `transfer_status`

**Key Features:**
- ✅ Optimistic locking (version column)
- ✅ Idempotency key for safe retries
- ✅ Constraints prevent invalid states
- ✅ Indexes on frequently queried columns
- ✅ Automatic `updated_at` timestamp

**Application Startup:**
```sql
-- Verify schema
SELECT * FROM pg_tables WHERE tablename IN ('wallets', 'transfers');

-- Create test wallet
INSERT INTO wallets (id, balance) VALUES (gen_random_uuid(), 10000);

-- Create test transfer
INSERT INTO transfers (
    id, idempotency_key, from_wallet_id, to_wallet_id, 
    amount, status
) VALUES (
    gen_random_uuid(),
    'test-transfer-123',
    'wallet-1',
    'wallet-2',
    1000,
    'PENDING'
);
```

---

### V002: Ledger Entries and Idempotency Records

**Tables Created:**
- `ledger_entries`: Immutable double-entry bookkeeping log
- `idempotency_records`: Request deduplication and response caching
- Views: `v_wallet_reconciliation`, `v_transfer_stats`

**Key Features:**
- ✅ Double-entry constraints (exactly 1 DEBIT + 1 CREDIT per transfer)
- ✅ Immutable audit trail (INSERT-only)
- ✅ Idempotency with request hash verification
- ✅ Status tracking for recovery (PENDING, COMPLETED, FAILED)
- ✅ Reconciliation views for data integrity checking

**Application Usage:**
```go
// Pseudo-code: Execute transfer with idempotency

func (s *TransferService) Execute(ctx context.Context, req TransferRequest) (*Transfer, error) {
    // 1. Check idempotency
    record, err := idemRepo.GetByKey(ctx, req.IdempotencyKey)
    if err == nil {
        // Already processed: return stored response
        return getTransferFromResponse(record.ResponseJSON)
    }
    if err != ErrNotFound {
        return nil, err
    }

    // 2. Create transfer (within transaction)
    transfer, err := executeTransfer(ctx, req)
    if err != nil {
        // 3. Store failure response
        idemRepo.UpdateResponse(ctx, 
            req.IdempotencyKey,
            encodeError(err),
            500,
            "FAILED")
        return nil, err
    }

    // 4. Create ledger entries
    ledgerRepo.CreateEntries(ctx, []LedgerEntry{
        {TransferID: transfer.ID, WalletID: req.FromWalletID, Type: DEBIT, Amount: req.Amount},
        {TransferID: transfer.ID, WalletID: req.ToWalletID, Type: CREDIT, Amount: req.Amount},
    })

    // 5. Store success response
    idemRepo.UpdateResponse(ctx,
        req.IdempotencyKey,
        encodeResponse(transfer),
        200,
        "COMPLETED")

    return transfer, nil
}
```

**Reconciliation Query:**
```sql
-- Check for discrepancies
SELECT * FROM v_wallet_reconciliation
WHERE stored_balance <> calculated_balance;

-- If discrepancy found, investigate:
SELECT * FROM ledger_entries
WHERE wallet_id = 'affected-wallet-id'
ORDER BY created_at DESC;

-- Recover from ledger if needed:
UPDATE wallets
SET balance = (
    SELECT COALESCE(SUM(CASE 
        WHEN entry_type = 'CREDIT' THEN amount 
        WHEN entry_type = 'DEBIT' THEN -amount 
    END), 0)
    FROM ledger_entries
    WHERE wallet_id = 'affected-wallet-id'
)
WHERE id = 'affected-wallet-id';
```

---

### V003: Audit Logging and Scalability (Optional)

**Tables Created:**
- `audit_log`: Tracks changes to wallets and transfers

**Views Created:**
- `mv_wallet_daily_activity`: Daily summary by wallet
- `mv_transfer_hourly_stats`: Hourly transfer statistics

**Features:**
- ✅ Automatic audit triggers (balance/status changes)
- ✅ Materialized views for fast analytics queries
- ✅ Archive functions for data retention
- ✅ Data integrity verification functions
- ✅ Query performance monitoring helpers

**Application Usage:**
```sql
-- Monitor wallet balance changes
SELECT * FROM audit_log
WHERE table_name = 'wallets' AND row_id = 'wallet-123'
ORDER BY changed_at DESC;

-- Example output:
-- id | table_name | row_id | operation | old_values | new_values | changed_at
-- 1  | wallets    | uuid-1 | UPDATE    | {"balance": 5000} | {"balance": 4500} | 2024-01-15 10:30:00

-- Refresh analytics (run hourly via cron or scheduled task)
REFRESH MATERIALIZED VIEW CONCURRENTLY mv_wallet_daily_activity;

-- Query analytics
SELECT * FROM mv_wallet_daily_activity
WHERE activity_date = CURRENT_DATE
ORDER BY transfer_count DESC;

-- Archive old data (run monthly)
SELECT archive_old_ledger_entries();
```

---

## Common Operations

### Verify Schema Installation

```sql
-- Check all migrations applied
SELECT table_name FROM information_schema.tables
WHERE table_schema = 'public'
ORDER BY table_name;

-- Should include:
-- - wallets
-- - transfers
-- - ledger_entries
-- - idempotency_records
-- - audit_log (if V003 applied)

-- View all indexes
\di public.*

-- View table sizes
SELECT
    tablename,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) as size
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;
```

### Data Validation

```sql
-- Run integrity checks (if V003 applied)
SELECT * FROM verify_data_integrity();

-- Manual checks:

-- 1. No negative balances
SELECT * FROM wallets WHERE balance < 0;

-- 2. No zero amounts
SELECT * FROM transfers WHERE amount <= 0;
SELECT * FROM ledger_entries WHERE amount <= 0;

-- 3. Balanced ledger (total debits = total credits)
SELECT
    (SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE entry_type = 'DEBIT') as total_debits,
    (SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE entry_type = 'CREDIT') as total_credits;

-- 4. Each transfer has exactly 2 entries (1 debit, 1 credit)
SELECT transfer_id, COUNT(*) as entry_count, COUNT(DISTINCT entry_type) as types
FROM ledger_entries
GROUP BY transfer_id
HAVING COUNT(*) <> 2 OR COUNT(DISTINCT entry_type) <> 2;
```

### Rollback Strategy

⚠️ **Important**: Once data is written, migrations cannot be safely reversed!

```sql
-- DO NOT delete tables (data loss!)
-- Instead: create new schema, migrate forward

-- Create new schema for testing
CREATE SCHEMA wallet_transfer_v2;
SET search_path TO wallet_transfer_v2;

-- Re-run migrations from V001
\i migrations/V001__init_wallets_and_transfers.sql
\i migrations/V002__init_ledger_and_idempotency.sql

-- Test with new schema
-- If successful, SWAP production connections to v2

-- Fall back to v1 if needed
SET search_path TO wallet_transfer_v1;  -- Original schema

-- Eventually drop old schema (after validation period)
-- DROP SCHEMA wallet_transfer_v1 CASCADE;
```

### Performance Monitoring After Migration

```sql
-- Monitor query performance
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

-- Find slow queries
SELECT query, calls, mean_time, max_time
FROM pg_stat_statements
WHERE query NOT LIKE '%pg_stat_statements%'
ORDER BY mean_time DESC
LIMIT 10;

-- Analyze table statistics (improves query planner)
ANALYZE wallets;
ANALYZE transfers;
ANALYZE ledger_entries;
ANALYZE idempotency_records;

-- Monitor index usage
SELECT schemaname, tablename, indexname, idx_scan, idx_tup_read, idx_tup_fetch
FROM pg_stat_user_indexes
ORDER BY idx_scan DESC;
```

---

## Troubleshooting

### Migration Fails with "Table Already Exists"

```sql
-- If you re-ran migrations:
DROP TABLE IF EXISTS wallets CASCADE;
DROP TABLE IF EXISTS transfers CASCADE;
DROP TABLE IF EXISTS ledger_entries CASCADE;
DROP TABLE IF EXISTS idempotency_records CASCADE;

-- Then re-run migrations
```

### Foreign Key Constraint Violations

```sql
-- If inserting test data fails:

-- Check existing data
SELECT COUNT(*) FROM wallets;
SELECT COUNT(*) FROM transfers;

-- Ensure referenced wallets exist
SELECT * FROM wallets WHERE id = 'your-wallet-id';

-- Insert test wallet first
INSERT INTO wallets (id, balance) VALUES (gen_random_uuid(), 10000);
```

### Permission Denied Errors

```sql
-- Grant permissions to application user
GRANT USAGE ON SCHEMA public TO app_user;
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA public TO app_user;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO app_user;
```

### Out of Memory During Large Operations

```sql
-- Increase work_mem for this session
SET work_mem = '256MB';

-- Or in postgresql.conf:
# work_mem = 256MB

-- For very large tables, batch operations:
-- Instead of: UPDATE all 1B rows
-- Do: UPDATE rows WHERE created_at > NOW() - INTERVAL '1 day'
```

---

## Best Practices

1. **Always test in staging** before applying to production
2. **Back up database** before running migrations
3. **Monitor performance** after migrations (new indexes take time)
4. **Keep migrations small** (one logical change per version)
5. **Never delete data** in migrations (archive instead)
6. **Include rollback plan** even if not implemented
7. **Document data changes** (what data moved, transformed, etc.)
8. **Verify constraints** on existing data before migration
9. **Test with production-like data volume** (same rows, similar patterns)
10. **Monitor query plans** after migration (indexes may change execution strategy)

---

## Related Documentation

- [Schema Design Details](./SCHEMA_DESIGN.md)
- [PostgreSQL Migration Best Practices](https://www.postgresql.org/docs/current/sql-syntax.html)
- [ACID Properties](https://en.wikipedia.org/wiki/ACID)
- [golang-migrate Documentation](https://github.com/golang-migrate/migrate)
