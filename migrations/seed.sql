-- Dev/test fixtures (NOT a migration). Apply by hand or via `make seed`.
-- Fixed UUIDs so tests and curl calls can reference known wallets.
INSERT INTO wallet (id, balance, is_active) VALUES
    ('11111111-1111-1111-1111-111111111111', 1000.0000, TRUE),   -- funded source
    ('22222222-2222-2222-2222-222222222222',    0.0000, TRUE),   -- empty destination
    ('33333333-3333-3333-3333-333333333333',  500.0000, FALSE)   -- inactive (failure-path testing)
ON CONFLICT (id) DO NOTHING;
