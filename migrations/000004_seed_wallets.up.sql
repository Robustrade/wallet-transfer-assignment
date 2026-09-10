-- Seed test wallets for development and testing
INSERT INTO wallets (id, balance, currency) VALUES
    ('wallet_1', 100000, 'USD'),  -- $1000.00
    ('wallet_2', 50000, 'USD'),   -- $500.00
    ('wallet_3', 25000, 'USD')    -- $250.00
ON CONFLICT (id) DO NOTHING;
