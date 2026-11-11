BEGIN;
SELECT pg_advisory_xact_lock(734921001);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE singleton = true AND version = 1 AND dirty = false
    ) THEN
        RAISE EXCEPTION 'migration 000002 requires clean schema version 1';
    END IF;
END;
$$;

ALTER TABLE transfers ALTER COLUMN id SET DEFAULT gen_random_uuid();
UPDATE schema_migrations SET version = 2 WHERE singleton = true;
COMMIT;
