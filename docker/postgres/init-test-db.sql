-- Created on first Postgres volume init (docker compose up).
-- Keeps go test TRUNCATE off the hand-used `wallet` database.
CREATE DATABASE wallet_test OWNER wallet;
