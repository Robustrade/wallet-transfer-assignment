-- Wallets and starting balances are seeded: there is no create-wallet or top-up API.
INSERT INTO wallets (id, customer_id, name, status, balance)
VALUES ('wallet_1', '11111111-1111-1111-1111-111111111111', 'Alice', 'ACTIVE', 1000.0000),
       ('wallet_2', '22222222-2222-2222-2222-222222222222', 'Bob', 'ACTIVE', 500.0000),
       ('wallet_3', '33333333-3333-3333-3333-333333333333', 'Carol', 'ACTIVE', 0.0000),
       ('wallet_4', '44444444-4444-4444-4444-444444444444', 'Dave (blocked)', 'BLOCKED', 250.0000);
