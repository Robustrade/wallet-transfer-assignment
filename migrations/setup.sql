-- Run with psql, connected to the application's existing postgres database.
-- Required psql variables: application_database, test_database, test_admin_url.
-- Connection URLs are read from the process environment, not stored here.
\set ON_ERROR_STOP on
\getenv application_url DATABASE_URL
\getenv test_url TEST_DATABASE_URL

-- CREATE DATABASE cannot run inside a transaction. Quote identifiers safely.
SELECT format('CREATE DATABASE %I', :'application_database')
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'application_database')
\gexec

\connect :test_admin_url
SELECT format('CREATE DATABASE %I', :'test_database')
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'test_database')
\gexec

-- Verify test connectivity; test fixtures create their own isolated schemas.
\connect :test_url
SELECT current_database() AS test_database;

\connect :application_url
SELECT to_regclass('schema_migrations') IS NOT NULL AS has_migrations
\gset
\if :has_migrations
    SELECT EXISTS (
        SELECT FROM schema_migrations
        WHERE singleton = true AND version = 2 AND NOT dirty
    ) AS current_schema
    \gset
    \if :current_schema
        \echo Application schema is already at version 2.
    \else
        \echo Unsupported or dirty schema; inspect schema_migrations before continuing.
        \quit 1
    \endif
\else
    \ir 000001_initial.up.sql
\endif
-- Seed demo wallets without resetting balances on subsequent runs.
BEGIN;
INSERT INTO wallets (id, opening_balance, balance) VALUES
    ('wallet_1', 1000, 1000), ('wallet_2', 0, 0)
ON CONFLICT (id) DO NOTHING;
COMMIT;
SELECT version, dirty FROM schema_migrations;
