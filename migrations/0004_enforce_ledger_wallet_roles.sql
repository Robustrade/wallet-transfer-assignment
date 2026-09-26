-- A composite foreign key already pins each ledger entry to a transfer and
-- its amount. This trigger additionally pins the entry's wallet to the
-- transfer side represented by its type: DEBIT -> source, CREDIT ->
-- destination. A CHECK constraint cannot reference another table.
CREATE FUNCTION enforce_ledger_wallet_role()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_wallet text;
    destination_wallet text;
BEGIN
    SELECT from_wallet_id, to_wallet_id
      INTO source_wallet, destination_wallet
      FROM transfers
     WHERE id = NEW.transfer_id;

    -- Leave missing transfers to the existing composite foreign key so that
    -- it retains its precise referential-integrity error.
    IF NOT FOUND THEN
        RETURN NEW;
    END IF;

    IF (NEW.entry_type = 'DEBIT' AND NEW.wallet_id IS DISTINCT FROM source_wallet)
       OR (NEW.entry_type = 'CREDIT' AND NEW.wallet_id IS DISTINCT FROM destination_wallet) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ledger entry wallet does not match transfer role';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER ledger_entries_enforce_wallet_role
BEFORE INSERT OR UPDATE ON ledger_entries
FOR EACH ROW
EXECUTE FUNCTION enforce_ledger_wallet_role();
