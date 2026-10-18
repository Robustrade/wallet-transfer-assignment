-- Seed 100 wallets with random opening balances (0 - 999,999 minor units).
-- Idempotent: does nothing when wallets already exist, so re-running
-- `docker compose up` never changes existing balances.
INSERT INTO wallets (balance, created_at, updated_at)
SELECT
    FLOOR(RANDOM() * 1000000)::BIGINT AS balance,
    NOW(),
    NOW()
FROM generate_series(1, 100)
WHERE NOT EXISTS (SELECT 1 FROM wallets);
