-- Seed data for manual testing / local demos.
--
-- NOT a Flyway migration (it lives outside classpath:db/migration on purpose) so it never runs in
-- prod or in the integration tests. Run it AFTER the app has booted at least once, so Flyway has
-- already created the `wallets` table:
--
--   docker exec -i wallet-postgres psql -U wallet -d wallet < scripts/seed-wallets.sql
--
-- Idempotent: ON CONFLICT DO NOTHING means re-running it is safe and won't duplicate rows.
-- Ids are fixed and human-readable so they're easy to copy into POST /transfers requests.

INSERT INTO wallets (id, balance, currency) VALUES
    ('00000000-0000-0000-0000-000000000001', 100000.00, 'INR'),
    ('00000000-0000-0000-0000-000000000002',  50000.50, 'INR'),
    ('00000000-0000-0000-0000-000000000003',  25000.75, 'INR'),
    ('00000000-0000-0000-0000-000000000004',   1000.00, 'INR'),
    ('00000000-0000-0000-0000-000000000005',    100.25, 'INR'),
    ('00000000-0000-0000-0000-000000000006',      0.00, 'INR'),
    ('00000000-0000-0000-0000-000000000007', 999999.99, 'INR'),
    ('00000000-0000-0000-0000-000000000008',   7500.00, 'INR'),
    ('00000000-0000-0000-0000-000000000009',  20000.00, 'USD'),
    ('00000000-0000-0000-0000-000000000010',   3000.00, 'USD')
ON CONFLICT (id) DO NOTHING;
