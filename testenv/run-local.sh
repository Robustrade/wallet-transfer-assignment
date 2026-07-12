#!/usr/bin/env bash
#
# Run the service against a REAL Postgres.
#
# What it does:
#   1. starts Postgres in Docker (schema.sql is applied automatically by the
#      Postgres image via /docker-entrypoint-initdb.d/ on first init)
#   2. waits for the schema to be ready
#   3. seeds two wallets (wallet_1=1000, wallet_2=0)
#   4. starts the service in the foreground on :8080
#
# Postgres (and this script's foreground service) are torn down on Ctrl+C.
#
# Usage:
#   cd testenv && ./run-local.sh
#
# Then, from another shell:
#   curl -s http://localhost:8080/transfers \
#     -H 'Content-Type: application/json' \
#     -d '{"idempotencyKey":"demo-1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}'
#
# Requirements: docker (with compose plugin), go.

set -euo pipefail
cd "$(dirname "$0")"

export DATABASE_URL="postgres://wallet:wallet@host.docker.internal:5432/wallet?sslmode=disable"
export LISTEN_ADDR=":8080"
export LOG_LEVEL="INFO"

cleanup() {
    docker compose down -v >/dev/null 2>&1 || true
}
trap cleanup EXIT

psql_exec() { docker compose exec -T postgres psql -U wallet -d wallet "$@"; }

echo "==> [1/4] starting postgres (applies schema.sql via init scripts)"
docker compose up -d
until [ "$(docker inspect -f '{{.State.Health.Status}}' wallet_pg 2>/dev/null)" = "healthy" ]; do
    sleep 1
done

echo "==> [2/4] waiting for schema to be applied"
# pg_isready can report healthy while init scripts are still running, so wait
# until the schema (the wallets table) actually exists before seeding.
until psql_exec -tAc "SELECT to_regclass('public.wallets') IS NOT NULL" 2>/dev/null | grep -q '^t$'; do
    sleep 1
done

echo "==> [3/4] seeding wallets (wallet_1=1000, wallet_2=0)"
psql_exec < seed.sql >/dev/null

echo "==> [4/4] starting service on ${LISTEN_ADDR} (Ctrl+C to stop and tear down)"
cd ..
exec go run ./cmd/server
