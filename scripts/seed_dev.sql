-- Development seed data. Not a migration — run explicitly with `make seed`
-- (or `psql "$DATABASE_URL" -f scripts/seed_dev.sql`). Safe to run repeatedly.

INSERT INTO wallets (id, balance, currency)
VALUES
    ('wallet_1', 100000, 'USD'),
    ('wallet_2',      0, 'USD'),
    ('wallet_3',  50000, 'USD')
ON CONFLICT (id) DO NOTHING;
