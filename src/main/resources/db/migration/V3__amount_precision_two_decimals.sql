-- Narrow monetary columns from 4 to 2 decimal places: balances and amounts are whole minor
-- currency units (e.g. paise/cents), so 2-decimal scale matches the domain and the API contract
-- (CreateTransferRequest enforces @Digits(fraction = 2)). Existing values are rounded to scale 2.
ALTER TABLE wallets        ALTER COLUMN balance       TYPE NUMERIC(19, 2);
ALTER TABLE transfers      ALTER COLUMN amount        TYPE NUMERIC(19, 2);
ALTER TABLE ledger_entries ALTER COLUMN amount        TYPE NUMERIC(19, 2);
ALTER TABLE ledger_entries ALTER COLUMN balance_after TYPE NUMERIC(19, 2);
