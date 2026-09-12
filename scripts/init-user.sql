-- Provision least-privileged application role and test cleanup role for local development/Docker
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'wallet_app') THEN
        CREATE ROLE wallet_app WITH LOGIN PASSWORD 'wallet_app_password';
    END IF;
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'wallet_test_cleanup') THEN
        CREATE ROLE wallet_test_cleanup WITH LOGIN PASSWORD 'cleanup_password';
    END IF;
END $$;

-- Create test database if not exists
SELECT 'CREATE DATABASE wallet_test_db OWNER postgres'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'wallet_test_db')\gexec

-- Grant database connection
GRANT CONNECT ON DATABASE wallet_db TO wallet_app;
GRANT CONNECT ON DATABASE wallet_test_db TO wallet_app;
GRANT CONNECT ON DATABASE wallet_test_db TO wallet_test_cleanup;

\c wallet_db
-- Runtime least-privilege: schema USAGE only (no DDL/table dropping)
GRANT USAGE ON SCHEMA public TO wallet_app;
GRANT SELECT, INSERT, UPDATE ON TABLE wallets, transfers TO wallet_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE idempotency_records TO wallet_app;
GRANT SELECT, INSERT ON TABLE ledger_entries TO wallet_app;
GRANT SELECT ON TABLE schema_migrations TO wallet_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO wallet_app;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO wallet_app;
REVOKE UPDATE, DELETE, TRUNCATE ON TABLE ledger_entries FROM wallet_app;
REVOKE TRUNCATE ON ALL TABLES IN SCHEMA public FROM wallet_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE ON TABLES TO wallet_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO wallet_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO wallet_app;

\c wallet_test_db
-- Runtime least-privilege for wallet_app
GRANT USAGE ON SCHEMA public TO wallet_app;
GRANT SELECT, INSERT, UPDATE ON TABLE wallets, transfers TO wallet_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE idempotency_records TO wallet_app;
GRANT SELECT, INSERT ON TABLE ledger_entries TO wallet_app;
GRANT SELECT ON TABLE schema_migrations TO wallet_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO wallet_app;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO wallet_app;
REVOKE UPDATE, DELETE, TRUNCATE ON TABLE ledger_entries FROM wallet_app;
REVOKE TRUNCATE ON ALL TABLES IN SCHEMA public FROM wallet_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE ON TABLES TO wallet_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO wallet_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO wallet_app;

-- Maintenance & Test Cleanup role: has TRUNCATE and DML for resetting test database
GRANT USAGE ON SCHEMA public TO wallet_test_cleanup;
GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE ON ALL TABLES IN SCHEMA public TO wallet_test_cleanup;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE ON TABLES TO wallet_test_cleanup;



