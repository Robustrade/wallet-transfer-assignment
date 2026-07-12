-- Seed data for local endpoint checks.
-- Run AFTER the service has started once, because the service creates the tables
-- via GORM AutoMigrate on startup.
--
-- wallet_1 starts funded, wallet_2 starts empty.

INSERT INTO wallets (wallet_id, balance, created_at, updated_at)
VALUES
    ('wallet_1', 1000, NOW(), NOW()),
    ('wallet_2', 0, NOW(), NOW())
ON CONFLICT (wallet_id) DO UPDATE
    SET balance = EXCLUDED.balance,
        updated_at = NOW();
