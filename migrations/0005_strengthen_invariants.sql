-- Migration 0005: Consolidate and strengthen database-level transfer invariants
--
-- This migration groups all transfer/ledger integrity triggers into one place.
-- Previously these would have been spread across several migrations; collecting
-- them here makes the dependency ordering explicit and easier to audit:
--
--   1. enforce_ledger_wallet_role        – BEFORE INSERT/UPDATE on ledger_entries
--        Pins each ledger entry's wallet_id to the correct side of its transfer
--        (DEBIT → from_wallet_id, CREDIT → to_wallet_id).
--
--   2. prevent_transfer_wallet_update    – BEFORE UPDATE on transfers
--        Rejects wallet-side changes once ledger entries already reference the
--        transfer, keeping the ledger internally consistent.
--
--   3. enforce_ledger_transfer_processed – AFTER INSERT/UPDATE on ledger_entries
--        Deferrable constraint trigger; ensures ledger rows are only attached to
--        PROCESSED transfers (deferrable so the service can write the ledger
--        before the final status flip within the same transaction).
--
--   4. enforce_transfer_state_transition – BEFORE INSERT/UPDATE on transfers
--        Guards the status state machine: new rows must be PENDING; PROCESSED
--        and FAILED are terminal; PENDING may only move to PROCESSED or FAILED.
--
--   5. enforce_processed_transfer_has_ledger_pair – AFTER INSERT/UPDATE on transfers
--        Deferrable constraint trigger (INITIALLY DEFERRED); when a transfer
--        reaches PROCESSED it must have exactly one DEBIT and one CREDIT entry
--        each matching the transfer amount.
--
--   6. prevent_ledger_update             – BEFORE UPDATE on ledger_entries
--        Ledger entries are immutable; any UPDATE is unconditionally rejected.
--
--   7. recheck_ledger_pair_on_delete     – AFTER DELETE on ledger_entries
--        Deferrable constraint trigger; if a PROCESSED transfer's ledger pair
--        is incomplete after a deletion, rejects the change.
--
-- BEFORE triggers are created before AFTER/constraint triggers because the
-- BEFORE checks can short-circuit work before the heavier AFTER checks run.
-- Deferrable triggers are noted inline; the application sets them DEFERRED
-- inside every transfer transaction via SetLocalTransactionTimeoutsSQL.

-- ---------------------------------------------------------------------------
-- 1. enforce_ledger_wallet_role
--    Replaces the version introduced in 0004: adds the NOT FOUND guard so
--    that a missing transfer is left to the FK constraint rather than being
--    silently accepted.
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION enforce_ledger_wallet_role()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_wallet      text;
    destination_wallet text;
BEGIN
    SELECT from_wallet_id, to_wallet_id
      INTO source_wallet, destination_wallet
      FROM transfers
     WHERE id = NEW.transfer_id;

    -- Let the composite FK handle a missing transfer with its own precise error.
    IF NOT FOUND THEN
        RETURN NEW;
    END IF;

    IF (NEW.entry_type = 'DEBIT'  AND NEW.wallet_id IS DISTINCT FROM source_wallet)
    OR (NEW.entry_type = 'CREDIT' AND NEW.wallet_id IS DISTINCT FROM destination_wallet)
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ledger entry wallet does not match transfer role';
    END IF;

    RETURN NEW;
END;
$$;

-- Drop the trigger created by 0004 (same name) before re-creating it so
-- this migration is idempotent when re-run after a partial rollback.
DROP TRIGGER IF EXISTS ledger_entries_enforce_wallet_role ON ledger_entries;

CREATE TRIGGER ledger_entries_enforce_wallet_role
BEFORE INSERT OR UPDATE ON ledger_entries
FOR EACH ROW
EXECUTE FUNCTION enforce_ledger_wallet_role();

-- ---------------------------------------------------------------------------
-- 2. prevent_transfer_wallet_update
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION prevent_transfer_wallet_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    entry_count int;
BEGIN
    -- Only act when a wallet column is actually changing.
    IF (NEW.from_wallet_id IS NOT DISTINCT FROM OLD.from_wallet_id)
   AND (NEW.to_wallet_id   IS NOT DISTINCT FROM OLD.to_wallet_id)
    THEN
        RETURN NEW;
    END IF;

    SELECT COUNT(*) INTO entry_count
      FROM ledger_entries
     WHERE transfer_id = NEW.id;

    IF entry_count > 0 THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'cannot change wallet sides of a transfer that already has ledger entries';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER transfers_prevent_wallet_update
BEFORE UPDATE ON transfers
FOR EACH ROW
EXECUTE FUNCTION prevent_transfer_wallet_update();

-- ---------------------------------------------------------------------------
-- 3. enforce_ledger_transfer_processed
--    DEFERRABLE INITIALLY IMMEDIATE — the application defers it to allow the
--    ledger write to precede the status flip inside one transaction.
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION enforce_ledger_transfer_processed()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    transfer_status text;
BEGIN
    SELECT status INTO transfer_status
      FROM transfers
     WHERE id = NEW.transfer_id;

    -- Missing transfer: let the FK report a precise referential-integrity error.
    IF NOT FOUND THEN
        RETURN NEW;
    END IF;

    IF transfer_status <> 'PROCESSED' THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'ledger entry requires a PROCESSED transfer';
    END IF;

    RETURN NEW;
END;
$$;

CREATE CONSTRAINT TRIGGER ledger_entries_require_processed_transfer
AFTER INSERT OR UPDATE ON ledger_entries
DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW
EXECUTE FUNCTION enforce_ledger_transfer_processed();

-- ---------------------------------------------------------------------------
-- 4. enforce_transfer_state_transition
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION enforce_transfer_state_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    -- INSERT: new transfers must start as PENDING.
    IF TG_OP = 'INSERT' THEN
        IF NEW.status <> 'PENDING' THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'new transfers must start with status PENDING';
        END IF;
        RETURN NEW;
    END IF;

    -- UPDATE: no-op when status is unchanged.
    IF NEW.status IS NOT DISTINCT FROM OLD.status THEN
        RETURN NEW;
    END IF;

    -- UPDATE: terminal states are immutable.
    IF OLD.status IN ('PROCESSED', 'FAILED') THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'transfer status is terminal and cannot be changed';
    END IF;

    -- UPDATE: PENDING may only advance to PROCESSED or FAILED.
    IF NEW.status NOT IN ('PROCESSED', 'FAILED') THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'invalid transfer status transition';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER transfers_enforce_state_transition
BEFORE INSERT OR UPDATE ON transfers
FOR EACH ROW
EXECUTE FUNCTION enforce_transfer_state_transition();

-- ---------------------------------------------------------------------------
-- 5. enforce_processed_transfer_has_ledger_pair
--    DEFERRABLE INITIALLY DEFERRED — the ledger pair is written before the
--    status flip, so this check must run at commit time.
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION enforce_processed_transfer_has_ledger_pair()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    debit_count  int;
    credit_count int;
BEGIN
    IF NEW.status <> 'PROCESSED' THEN
        RETURN NEW;
    END IF;

    SELECT
        COUNT(*) FILTER (WHERE entry_type = 'DEBIT'  AND amount = NEW.amount),
        COUNT(*) FILTER (WHERE entry_type = 'CREDIT' AND amount = NEW.amount)
      INTO debit_count, credit_count
      FROM ledger_entries
     WHERE transfer_id = NEW.id;

    IF debit_count <> 1 OR credit_count <> 1 THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'PROCESSED transfer must have exactly one matching DEBIT and one CREDIT ledger entry';
    END IF;

    RETURN NEW;
END;
$$;

CREATE CONSTRAINT TRIGGER transfers_processed_requires_ledger_pair
AFTER INSERT OR UPDATE ON transfers
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION enforce_processed_transfer_has_ledger_pair();

-- ---------------------------------------------------------------------------
-- 6. prevent_ledger_update
--    Ledger entries are append-only; updates are unconditionally rejected.
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION prevent_ledger_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION USING
        ERRCODE = '23514',
        MESSAGE = 'ledger entries are immutable and cannot be updated';
END;
$$;

CREATE TRIGGER ledger_entries_prevent_update
BEFORE UPDATE ON ledger_entries
FOR EACH ROW
EXECUTE FUNCTION prevent_ledger_update();

-- ---------------------------------------------------------------------------
-- 7. recheck_ledger_pair_on_delete
--    DEFERRABLE INITIALLY IMMEDIATE — re-validates the DEBIT+CREDIT pair
--    after any row is removed from ledger_entries.
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION recheck_ledger_pair_on_delete()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    transfer_status text;
    debit_count     int;
    credit_count    int;
BEGIN
    SELECT status INTO transfer_status
      FROM transfers
     WHERE id = OLD.transfer_id;

    -- Transfer already gone — nothing left to protect.
    IF NOT FOUND THEN
        RETURN OLD;
    END IF;

    IF transfer_status <> 'PROCESSED' THEN
        RETURN OLD;
    END IF;

    SELECT
        COUNT(*) FILTER (WHERE entry_type = 'DEBIT'),
        COUNT(*) FILTER (WHERE entry_type = 'CREDIT')
      INTO debit_count, credit_count
      FROM ledger_entries
     WHERE transfer_id = OLD.transfer_id;

    IF debit_count < 1 OR credit_count < 1 THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'deleting this ledger entry would leave a PROCESSED transfer without a complete DEBIT/CREDIT pair';
    END IF;

    RETURN OLD;
END;
$$;

CREATE CONSTRAINT TRIGGER ledger_entries_recheck_pair_on_delete
AFTER DELETE ON ledger_entries
DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW
EXECUTE FUNCTION recheck_ledger_pair_on_delete();
