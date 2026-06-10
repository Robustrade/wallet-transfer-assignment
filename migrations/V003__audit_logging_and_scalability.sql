-- =====================================================================
-- V003: Add Audit Logging and Scalability Features (Optional)
-- =====================================================================
-- This migration adds enhanced audit logging, materialized views for
-- analytics, and partitioning support for future horizontal scaling.
--
-- ⚠️ OPTIONAL: Apply only if you need advanced audit/analytics features
-- Base system works perfectly with V001 + V002
-- =====================================================================

-- =====================================================================
-- AUDIT LOG TABLE
-- =====================================================================
-- Tracks all changes to critical tables (wallets, transfers)
-- Supports compliance requirements and debugging

CREATE TABLE audit_log (
    id BIGSERIAL PRIMARY KEY,

    -- Table being audited
    table_name TEXT NOT NULL,

    -- Row ID (UUID converted to TEXT for flexibility)
    row_id TEXT NOT NULL,

    -- Operation: INSERT, UPDATE, DELETE
    operation TEXT NOT NULL
        CONSTRAINT chk_audit_operation_valid
            CHECK (operation IN ('INSERT', 'UPDATE', 'DELETE')),

    -- Who made the change (application user/service)
    changed_by TEXT,

    -- What changed: new values (JSON)
    new_values JSONB,

    -- What changed: old values (JSON) - for updates only
    old_values JSONB,

    -- When it happened
    changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Audit trail is immutable (marked at insert)
    -- Note: Check constraints cannot reference functions like NOW() in all DB versions
    -- This constraint is informational; enforcement is via application logic or trigger
);

-- Indexes for audit queries
CREATE INDEX idx_audit_table_row
    ON audit_log(table_name, row_id, changed_at DESC);

CREATE INDEX idx_audit_changed_by
    ON audit_log(changed_by, changed_at DESC);

-- Trigger to automatically log wallet changes
CREATE OR REPLACE FUNCTION audit_wallet_changes()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        INSERT INTO audit_log (table_name, row_id, operation, new_values, changed_by)
        VALUES ('wallets', NEW.id::TEXT, 'INSERT', row_to_json(NEW), 'system');
    ELSIF TG_OP = 'UPDATE' THEN
        IF OLD.balance <> NEW.balance OR OLD.version <> NEW.version THEN
            INSERT INTO audit_log (table_name, row_id, operation, old_values, new_values, changed_by)
            VALUES ('wallets', NEW.id::TEXT, 'UPDATE', row_to_json(OLD), row_to_json(NEW), 'system');
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER wallets_audit_trigger
    AFTER INSERT OR UPDATE ON wallets
    FOR EACH ROW
    EXECUTE FUNCTION audit_wallet_changes();

-- Similar trigger for transfers (optional, be selective due to volume)
CREATE OR REPLACE FUNCTION audit_transfer_changes()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        INSERT INTO audit_log (table_name, row_id, operation, new_values, changed_by)
        VALUES ('transfers', NEW.id::TEXT, 'INSERT', row_to_json(NEW), 'system');
    ELSIF TG_OP = 'UPDATE' AND NEW.status <> OLD.status THEN
        -- Only log status changes to reduce volume
        INSERT INTO audit_log (table_name, row_id, operation, old_values, new_values, changed_by)
        VALUES ('transfers', NEW.id::TEXT, 'UPDATE',
                jsonb_build_object('status', OLD.status),
                jsonb_build_object('status', NEW.status),
                'system');
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER transfers_audit_trigger
    AFTER INSERT OR UPDATE ON transfers
    FOR EACH ROW
    EXECUTE FUNCTION audit_transfer_changes();

-- =====================================================================
-- MATERIALIZED VIEWS FOR ANALYTICS
-- =====================================================================
-- Pre-computed aggregations to speed up reporting
-- Refresh periodically (e.g., every hour via cron)

-- Daily wallet activity summary
CREATE MATERIALIZED VIEW mv_wallet_daily_activity AS
SELECT
    DATE_TRUNC('day', t.created_at)::DATE as activity_date,
    CASE
        WHEN t.from_wallet_id = le.wallet_id THEN t.from_wallet_id
        ELSE t.to_wallet_id
    END as wallet_id,
    COUNT(*) as transfer_count,
    SUM(t.amount) as total_amount,
    SUM(CASE WHEN t.status = 'PROCESSED' THEN 1 ELSE 0 END) as successful_count,
    SUM(CASE WHEN t.status = 'FAILED' THEN 1 ELSE 0 END) as failed_count
FROM transfers t
JOIN ledger_entries le ON t.id = le.transfer_id
GROUP BY DATE_TRUNC('day', t.created_at), wallet_id;

CREATE INDEX idx_mv_wallet_activity_date
    ON mv_wallet_daily_activity(activity_date DESC, wallet_id);

-- Transfer status distribution (hourly)
CREATE MATERIALIZED VIEW mv_transfer_hourly_stats AS
SELECT
    DATE_TRUNC('hour', created_at)::TIMESTAMP as hour,
    status,
    COUNT(*) as count,
    AVG(EXTRACT(EPOCH FROM (updated_at - created_at))) as avg_duration_seconds,
    MAX(EXTRACT(EPOCH FROM (updated_at - created_at))) as max_duration_seconds
FROM transfers
WHERE created_at > NOW() - INTERVAL '90 days'  -- Keep recent data only
GROUP BY DATE_TRUNC('hour', created_at), status;

CREATE INDEX idx_mv_transfer_stats_hour
    ON mv_transfer_hourly_stats(hour DESC);

-- Refresh materialized views on a schedule
-- (Manually or via pgAgent: SELECT pg_sleep(3600); REFRESH MATERIALIZED VIEW CONCURRENTLY mv_wallet_daily_activity;)
-- Or implement in application layer (e.g., cron job)

-- =====================================================================
-- ARCHIVAL & RETENTION POLICIES
-- =====================================================================
-- Support for long-term data retention and compliance

-- Create archive table for old ledger entries
CREATE TABLE ledger_entries_archive (
    LIKE ledger_entries INCLUDING ALL
);

-- Function to archive old ledger entries (run periodically)
CREATE OR REPLACE FUNCTION archive_old_ledger_entries()
RETURNS TABLE (archived_rows BIGINT) AS $$
DECLARE
    v_archived BIGINT;
BEGIN
    -- Move entries older than 2 years to archive
    WITH moved AS (
        DELETE FROM ledger_entries
        WHERE created_at < NOW() - INTERVAL '2 years'
        RETURNING *
    )
    INSERT INTO ledger_entries_archive
    SELECT * FROM moved;

    GET DIAGNOSTICS v_archived = ROW_COUNT;
    RETURN QUERY SELECT v_archived;
END;
$$ LANGUAGE plpgsql;

-- Example usage (run monthly):
-- SELECT archive_old_ledger_entries();

-- =====================================================================
-- PERFORMANCE OPTIMIZATION HINTS
-- =====================================================================

-- Statistics for query planner
ANALYZE wallets;
ANALYZE transfers;
ANALYZE ledger_entries;
ANALYZE idempotency_records;

-- For large tables, enable parallelization
ALTER TABLE ledger_entries SET (parallel_workers = 4);
ALTER TABLE transfers SET (parallel_workers = 4);

-- =====================================================================
-- SECURITY & ACCESS CONTROL
-- =====================================================================
-- Optional: Implement row-level security (RLS)

-- Create roles for different access levels
-- For multi-tenant deployments, consider implementing Row Level Security (RLS)
-- 
-- GRANT SELECT, INSERT, UPDATE ON wallets, transfers, ledger_entries
--    TO app_user;
-- GRANT SELECT, INSERT, UPDATE, DELETE ON wallets, transfers
--    TO admin_user;
-- GRANT EXECUTE ON FUNCTION archive_old_ledger_entries() TO admin_user;

-- =====================================================================
-- MONITORING & ALERTING SETUP
-- =====================================================================

-- Table size monitoring (query periodically)
CREATE OR REPLACE FUNCTION get_table_sizes()
RETURNS TABLE (
    table_name TEXT,
    size_bytes BIGINT,
    size_mb NUMERIC
) AS $$
SELECT
    tablename,
    pg_total_relation_size(schemaname||'.'||tablename)::BIGINT,
    (pg_total_relation_size(schemaname||'.'||tablename) / 1024.0 / 1024.0)::NUMERIC(10,2)
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;
$$ LANGUAGE SQL;

-- Usage: SELECT * FROM get_table_sizes();

-- Query performance metrics (for monitoring dashboards)
CREATE OR REPLACE FUNCTION get_query_performance_metrics()
RETURNS TABLE (
    query TEXT,
    calls BIGINT,
    total_time NUMERIC,
    avg_time NUMERIC,
    max_time NUMERIC
) AS $$
SELECT
    query,
    calls,
    total_time::NUMERIC(10,3),
    (total_time / calls)::NUMERIC(10,6),
    max_time::NUMERIC(10,3)
FROM pg_stat_statements
WHERE query NOT LIKE '%pg_stat_statements%'
ORDER BY total_time DESC
LIMIT 20;
$$ LANGUAGE SQL;

-- =====================================================================
-- SCALABILITY PREPARATION: PARTITIONING
-- =====================================================================
-- NOT REQUIRED for current scale, but prepared for future growth
--
-- When ledger_entries table exceeds 1TB or queries become slow,
-- apply time-based partitioning:
--
-- ALTER TABLE ledger_entries RENAME TO ledger_entries_old;
--
-- CREATE TABLE ledger_entries (
--     id UUID PRIMARY KEY,
--     transfer_id UUID NOT NULL,
--     wallet_id UUID NOT NULL,
--     entry_type ledger_entry_type NOT NULL,
--     amount BIGINT NOT NULL CHECK (amount > 0),
--     created_at TIMESTAMPTZ NOT NULL,
--     PARTITION BY RANGE (created_at)
-- ) INHERITS (ledger_entries_old);
--
-- CREATE TABLE ledger_entries_2024_01 PARTITION OF ledger_entries
--     FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');
--
-- CREATE TABLE ledger_entries_2024_02 PARTITION OF ledger_entries
--     FOR VALUES FROM ('2024-02-01') TO ('2024-03-01');
-- ... and so on
--
-- Then:
-- INSERT INTO ledger_entries SELECT * FROM ledger_entries_old;
-- DROP TABLE ledger_entries_old;

-- =====================================================================
-- TESTING & VALIDATION
-- =====================================================================

-- Verify data integrity (run periodically)
CREATE OR REPLACE FUNCTION verify_data_integrity()
RETURNS TABLE (
    check_name TEXT,
    status TEXT,
    detail TEXT
) AS $$
BEGIN
    -- Check 1: No negative balances
    IF EXISTS (SELECT 1 FROM wallets WHERE balance < 0) THEN
        INSERT INTO (check_name, status, detail) VALUES
            ('negative_balances', 'FAIL', 'Wallets with negative balance found');
    ELSE
        INSERT INTO (check_name, status, detail) VALUES
            ('negative_balances', 'PASS', 'All balances non-negative');
    END IF;

    -- Check 2: Balance reconciliation
    IF EXISTS (SELECT * FROM v_wallet_reconciliation WHERE balance_discrepancy <> 0) THEN
        INSERT INTO (check_name, status, detail) VALUES
            ('balance_reconciliation', 'FAIL', 'Wallet balance discrepancies detected');
    ELSE
        INSERT INTO (check_name, status, detail) VALUES
            ('balance_reconciliation', 'PASS', 'All wallets reconciled');
    END IF;

    -- Check 3: Ledger balance equals transfers
    IF (SELECT SUM(amount) FROM ledger_entries WHERE entry_type = 'DEBIT') <>
       (SELECT SUM(amount) FROM ledger_entries WHERE entry_type = 'CREDIT') THEN
        INSERT INTO (check_name, status, detail) VALUES
            ('ledger_balance', 'FAIL', 'Total debits <> total credits');
    ELSE
        INSERT INTO (check_name, status, detail) VALUES
            ('ledger_balance', 'PASS', 'Ledger balanced');
    END IF;

    -- Check 4: Each transfer has exactly 2 ledger entries
    IF EXISTS (
        SELECT transfer_id FROM ledger_entries
        GROUP BY transfer_id
        HAVING COUNT(*) <> 2 OR COUNT(DISTINCT entry_type) <> 2
    ) THEN
        INSERT INTO (check_name, status, detail) VALUES
            ('ledger_entries_per_transfer', 'FAIL', 'Some transfers missing debit or credit entry');
    ELSE
        INSERT INTO (check_name, status, detail) VALUES
            ('ledger_entries_per_transfer', 'PASS', 'All transfers have balanced entries');
    END IF;

    RETURN QUERY
    SELECT * FROM temp_check_results;
END;
$$ LANGUAGE plpgsql;

-- Usage: SELECT * FROM verify_data_integrity();
