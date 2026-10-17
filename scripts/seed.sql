-- Seed script: scripts/seed.sql
-- Sets up initial development users and wallets for testing and local exploration.

INSERT INTO users (id, name, created_at)
VALUES 
    ('user_1', 'Alice Smith', NOW()),
    ('user_2', 'Bob Jones', NOW())
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name;

INSERT INTO wallets (id, user_id, balance, currency, created_at, updated_at)
VALUES 
    ('wallet_1', 'user_1', 1000000, 'INR', NOW(), NOW()), -- ₹10,000.00 (1000000 paise)
    ('wallet_2', 'user_2', 500000,  'INR', NOW(), NOW())  -- ₹5,000.00  (500000 paise)
ON CONFLICT (id) DO UPDATE 
    SET balance = EXCLUDED.balance,
        updated_at = NOW();
