BEGIN;

-- Per-wallet history queries (GET /wallets/{id}/transfers) filter by
-- from_wallet_id OR to_wallet_id and ORDER BY created_at DESC. Two
-- composite indexes let Postgres BitmapOr-merge the two halves
-- efficiently without a sequential scan even on large transfer tables.
CREATE INDEX transfers_from_wallet_created_idx
    ON transfers (from_wallet_id, created_at DESC, id DESC);
CREATE INDEX transfers_to_wallet_created_idx
    ON transfers (to_wallet_id,   created_at DESC, id DESC);

-- The id DESC suffix breaks ties when two transfers share a created_at
-- timestamp (BIGSERIAL clocks can collide at microsecond granularity
-- under heavy load). It also makes the cursor-based pagination
-- deterministic.

COMMIT;
