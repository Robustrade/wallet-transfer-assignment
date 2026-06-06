-- 000004_add_nature_wallet.up.sql
-- Seeds the system "nature" wallet used as the source for magic deposits.
-- The wallet is created with a static UUID so that application code can
-- reference it via the domain.NatureWalletID constant without any look-up.
-- Balance is set to 1 000 000 cents ($10 000.00) to ensure ample funds for
-- random deposits in the range [100, 10 000] cents.

BEGIN;

INSERT INTO wallets (id, balance, version)
VALUES ('c0ffee00-0000-0000-0000-000000000001', 1000000, 0)
ON CONFLICT (id) DO NOTHING;

COMMIT;
