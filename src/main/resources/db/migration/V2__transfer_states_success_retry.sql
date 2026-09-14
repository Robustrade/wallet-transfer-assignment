-- Align transfer states with service flow: PENDING, RETRY, FAILED, SUCCESS
ALTER TABLE transfers DROP CONSTRAINT IF EXISTS transfers_state_ck;
UPDATE transfers SET state = 'SUCCESS' WHERE state = 'PROCESSED';
ALTER TABLE transfers
    ADD CONSTRAINT transfers_state_ck
    CHECK (state IN ('PENDING', 'RETRY', 'FAILED', 'SUCCESS'));
