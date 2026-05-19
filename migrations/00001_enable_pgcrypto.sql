-- +goose Up
-- pgcrypto provides gen_random_uuid() used by transfers/ledger_entries PKs.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- +goose Down
DROP EXTENSION IF EXISTS pgcrypto;
