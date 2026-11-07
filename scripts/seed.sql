-- Demo wallets for local runs (make seed). Re-running resets their balances.
INSERT INTO wallets (id, balance) VALUES
    ('wallet_1', 10000),
    ('wallet_2', 5000),
    ('wallet_3', 0)
ON CONFLICT (id) DO UPDATE SET balance = EXCLUDED.balance, updated_at = now();
