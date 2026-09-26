-- Drop two speculative indexes that no query uses.
--
-- transfers are always read by primary key (id); there is no access path that
-- filters or sorts by from_wallet_id or to_wallet_id. The indexes therefore
-- pay write cost on every transfer insert (and on cascade from wallet deletes)
-- without ever being chosen by the planner.
--
-- If an operational access path ("recent transfers for wallet X") ever appears,
-- re-add them with CREATE INDEX CONCURRENTLY so production traffic is not
-- blocked; the definitions live in 0001_init.sql for reference.
DROP INDEX transfers_from_wallet_idx;
DROP INDEX transfers_to_wallet_idx;
