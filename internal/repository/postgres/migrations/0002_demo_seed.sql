-- Demo wallets for `make run` / `make demo`. Applied only when seeding is
-- enabled (the server does, the tests do not).

INSERT INTO wallets (id, balance, currency, created_at, updated_at) VALUES
    ('wallet_1',   100000, 'SGD', '2026-01-01T00:00:00.000000Z', '2026-01-01T00:00:00.000000Z'),
    ('wallet_2',    50000, 'SGD', '2026-01-01T00:00:00.000000Z', '2026-01-01T00:00:00.000000Z'),
    ('wallet_3',        0, 'SGD', '2026-01-01T00:00:00.000000Z', '2026-01-01T00:00:00.000000Z'),
    ('wallet_usd',  10000, 'USD', '2026-01-01T00:00:00.000000Z', '2026-01-01T00:00:00.000000Z');
