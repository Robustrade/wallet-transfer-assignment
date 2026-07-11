-- Manual/demo fixtures only — NOT a golang-migrate migration, schema
-- migrations stay data-free. Run via `make seed`. Safe to re-run:
-- inserts are idempotent (ON CONFLICT DO NOTHING) and never touch a
-- wallet's balance if it already exists, so re-seeding never resets
-- balances a demo session has already spent.
--
-- These exact UUIDs and starting balances are documented in README.md —
-- keep them in sync if you change either here.
INSERT INTO wallets (id, balance, created_at, updated_at) VALUES
    ('018f0000-0000-7000-8000-000000000001', 1000.00, now(), now()), -- Alice
    ('018f0000-0000-7000-8000-000000000002', 500.00, now(), now()),  -- Bob
    ('018f0000-0000-7000-8000-000000000003', 0.00, now(), now())     -- Carol (zero balance, for INSUFFICIENT_BALANCE demos)
ON CONFLICT (id) DO NOTHING;
