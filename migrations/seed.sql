-- Demo wallets only; this does not reset or fund existing wallets on rerun.
BEGIN;
INSERT INTO wallets (id, opening_balance, balance) VALUES
    ('wallet_1', 1000, 1000), ('wallet_2', 0, 0)
ON CONFLICT (id) DO NOTHING;
COMMIT;
