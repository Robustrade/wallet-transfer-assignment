-- Clear existing data (optional, useful for repeatable runs)
TRUNCATE TABLE ledger_entries RESTART IDENTITY CASCADE;
TRUNCATE TABLE transfers RESTART IDENTITY CASCADE;
TRUNCATE TABLE wallets RESTART IDENTITY CASCADE;
TRUNCATE TABLE idempotency_records RESTART IDENTITY CASCADE;


-- Insert sample wallets
INSERT INTO wallets (balance, created_at, updated_at)
VALUES 
    (1000, NOW(), NOW()),  -- wallet 1
    (500, NOW(), NOW());   -- wallet 2
