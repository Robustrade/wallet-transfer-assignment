-- =============================================================
-- Wallet Transfer — Seed Data (Development/Testing Only)
-- =============================================================
-- This file is NOT part of the production schema migration.
-- Run this manually or in test setup to populate demo wallets.

INSERT OR IGNORE INTO wallets (user_id, currency, balance) VALUES
    (1, 'USD', '1000.00000000'),
    (2, 'USD', '500.00000000');
