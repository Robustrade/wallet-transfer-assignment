-- Rename terminal transfer state SUCCESS → PROCESSED (ASSIGNMENT.md naming).
UPDATE transfers SET state = 'PROCESSED' WHERE state = 'SUCCESS';

ALTER TABLE transfers DROP CONSTRAINT IF EXISTS transfers_state_ck;
ALTER TABLE transfers
    ADD CONSTRAINT transfers_state_ck
    CHECK (state IN ('PENDING', 'RETRY', 'FAILED', 'PROCESSED'));
