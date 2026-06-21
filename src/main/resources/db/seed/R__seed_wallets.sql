-- Dev-only seed data for local testing. Not for production use.
-- Repeatable migration: re-runs whenever checksum changes, always after versioned migrations.
INSERT INTO wallets (id, balance, created_at, updated_at) VALUES
    ('wallet_1', 10000, NOW(), NOW()),
    ('wallet_2', 5000, NOW(), NOW()),
    ('wallet_3', 2500, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;
