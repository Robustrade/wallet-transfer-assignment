-- Development reset ONLY; drops all wallet, transfer and ledger data.
BEGIN;
DROP TABLE IF EXISTS ledger_entries;
DROP TABLE IF EXISTS transfers;
DROP TABLE IF EXISTS wallets;
DROP FUNCTION IF EXISTS reject_ledger_mutation();
DROP FUNCTION IF EXISTS reject_opening_balance_change();
COMMIT;
