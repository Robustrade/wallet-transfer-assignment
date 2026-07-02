-- Balances are whole rupees (INR): 100000 = Rs 1,00,000
INSERT INTO wallets (id, balance, created_at, updated_at)
VALUES
    ('wallet-alice', 100000, NOW(), NOW()),
    ('wallet-bob', 50000, NOW(), NOW());
