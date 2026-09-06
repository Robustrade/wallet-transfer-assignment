-- Defense-in-depth: even if application code has a bug, Postgres itself
-- rejects illegal state transitions (PROCESSED/FAILED are terminal).
CREATE OR REPLACE FUNCTION enforce_transfer_state_transition()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.status = 'PROCESSED' AND NEW.status <> 'PROCESSED' THEN
        RAISE EXCEPTION 'invalid transfer state transition: % -> %', OLD.status, NEW.status;
    END IF;
    IF OLD.status = 'FAILED' AND NEW.status <> 'FAILED' THEN
        RAISE EXCEPTION 'invalid transfer state transition: % -> %', OLD.status, NEW.status;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_transfer_state_transition
    BEFORE UPDATE ON transfers
    FOR EACH ROW
    EXECUTE FUNCTION enforce_transfer_state_transition();
