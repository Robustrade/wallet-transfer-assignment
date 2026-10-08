BEGIN;

-- Break the circular keyed-transfer relationship before dropping the tables.
ALTER TABLE transfers DROP CONSTRAINT transfers_idempotency_record_fk;
DROP TABLE idempotency_records;
DROP TABLE ledger_entries;
DROP TABLE transfers;
DROP TABLE wallets;

COMMIT;
