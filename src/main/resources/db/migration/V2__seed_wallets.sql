-- Demo wallets for manual testing. The assignment does not require a wallet-creation
-- endpoint, so wallets are pre-provisioned via this seed migration.
INSERT INTO wallets (id, balance) VALUES
    ('11111111-1111-1111-1111-111111111111', 1000.0000),
    ('22222222-2222-2222-2222-222222222222', 500.0000),
    ('33333333-3333-3333-3333-333333333333', 0.0000);
