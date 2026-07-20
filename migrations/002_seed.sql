INSERT INTO wallets (id, owner_id, currency, opening_balance, balance, status)
VALUES
    ('00000000-0000-0000-0000-000000000001', 'owner_1', 'USD', 10000, 10000, 'ACTIVE'),
    ('00000000-0000-0000-0000-000000000002', 'owner_2', 'USD', 5000, 5000, 'ACTIVE')
ON CONFLICT (id) DO NOTHING;
