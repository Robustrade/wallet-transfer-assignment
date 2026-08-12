-- Seeds the single system wallet this service ships with. Additional system
-- wallets (e.g. for other currencies) would be added the same way in a later
-- migration, not created dynamically through the API.
INSERT INTO wallets (id, kind, currency, balance_minor)
VALUES ('treasury_inr', 'SYSTEM', 'INR', 0)
ON CONFLICT (id) DO NOTHING;
