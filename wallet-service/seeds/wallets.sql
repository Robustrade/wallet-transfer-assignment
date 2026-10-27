-- Opening balances in minor units: INR 5.00 and INR 2.00.
-- Re-running seeds must never reset balances after transfers.
INSERT INTO wallets (id, currency_code, balance_minor)
VALUES ('wallet_1', 'INR', 500), ('wallet_2', 'INR', 200)
ON CONFLICT (id) DO NOTHING;
