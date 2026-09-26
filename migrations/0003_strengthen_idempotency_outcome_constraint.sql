-- A successful or insufficient-funds outcome must identify the transfer it
-- completed. Claimed and not-found outcomes must not. This migration applies
-- the invariant to databases created before the initial schema was tightened.
ALTER TABLE idempotency_records
    DROP CONSTRAINT IF EXISTS idem_transfer_requires_recorded_outcome,
    DROP CONSTRAINT IF EXISTS idem_notfound_has_no_transfer,
    DROP CONSTRAINT IF EXISTS idem_transfer_matches_outcome,
    ADD CONSTRAINT idem_transfer_matches_outcome
        CHECK (CASE
            WHEN response_status IN (201, 422) THEN transfer_id IS NOT NULL
            ELSE transfer_id IS NULL
        END);
