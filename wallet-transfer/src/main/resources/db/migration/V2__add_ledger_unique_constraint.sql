ALTER TABLE ledger_entries
    ADD CONSTRAINT uq_ledger_entries_transfer_type UNIQUE (transfer_id, type);