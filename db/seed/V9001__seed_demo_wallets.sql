-- Demo wallets (ADR-0010). Applied only when SEED_DEMO_DATA=true is passed to `just migrate`
-- (never in CI or testcontainers-backed tests, which never pass that flag). Stable, hardcoded
-- ids so scripts/simulate.py and manual docker-compose exploration can reference them directly
-- — there's no wallet-creation endpoint (deliberately out of scope, ADR-0010).
INSERT INTO wallets (id, balance) VALUES
    ('00000000-0000-7000-8000-000000000001', 100000),
    ('00000000-0000-7000-8000-000000000002', 50000),
    ('00000000-0000-7000-8000-000000000003', 25000),
    ('00000000-0000-7000-8000-000000000004', 0);
