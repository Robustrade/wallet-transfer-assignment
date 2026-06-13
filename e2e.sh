#!/usr/bin/env bash
set -euo pipefail

# End-to-end demo script for wallet-transfer service
# - starts the server in background
# - seeds wallets
# - performs a happy transfer and idempotent retry
# - performs an insufficient-funds request
# - runs a concurrent debit batch
# - prints verification SQL outputs

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT_DIR"

echo "== e2e: starting server"
TEST_RUN_DURATION_MS=60000 go run . > /tmp/wallet-transfer.log 2>&1 &
SERVER_PID=$!
echo "server pid=$SERVER_PID (log /tmp/wallet-transfer.log)"

trap 'echo "== e2e: stopping server"; kill $SERVER_PID >/dev/null 2>&1 || true' EXIT

echo -n "waiting for server on :8080.."
for i in {1..20}; do
  if curl -sS http://localhost:8080/ >/dev/null 2>&1; then
    echo " ok"
    break
  fi
  echo -n "."
  sleep 0.2
done

echo "== e2e: seeding DB (wallets.db)"
sqlite3 wallets.db "PRAGMA foreign_keys=ON; DELETE FROM ledger_entries; DELETE FROM transfers; DELETE FROM idempotency_records; DELETE FROM wallets;"
sqlite3 wallets.db "INSERT INTO wallets (id, balance) VALUES ('wallet_1', 1000);"
sqlite3 wallets.db "INSERT INTO wallets (id, balance) VALUES ('wallet_2', 100);"
sqlite3 wallets.db "INSERT INTO wallets (id, balance) VALUES ('c_src', 100);"
sqlite3 wallets.db "INSERT INTO wallets (id, balance) VALUES ('c_dst', 0);"

echo "== e2e: happy-path transfer"
RESP=$(curl -s -X POST http://localhost:8080/transfers -H 'Content-Type: application/json' -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}')
echo "response: $RESP"
TRANSFER_ID=$(echo "$RESP" | sed -n 's/.*"transferId":"\([^"]*\)".*/\1/p')
echo "transfer id: $TRANSFER_ID"

echo "== e2e: idempotent retry (same key)"
RESP2=$(curl -s -X POST http://localhost:8080/transfers -H 'Content-Type: application/json' -d '{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}')
echo "retry response: $RESP2"

echo "== e2e: insufficient funds request (expect 422)"
HTTP_CODE=$(curl -s -o /tmp/if_resp.txt -w "%{http_code}" -X POST http://localhost:8080/transfers -H 'Content-Type: application/json' -d '{"idempotencyKey":"no_money","fromWalletId":"wallet_2","toWalletId":"wallet_1","amount":100000}')
echo "http code: $HTTP_CODE"
cat /tmp/if_resp.txt || true

echo "== e2e: concurrent debits on c_src (10 attempts, 30 each)"
for i in $(seq 1 10); do
  KEY="con_${i}_$(date +%s%N)"
  PAYLOAD=$(printf '{"idempotencyKey":"%s","fromWalletId":"c_src","toWalletId":"c_dst","amount":30}' "$KEY")
  curl -s -X POST http://localhost:8080/transfers -H 'Content-Type: application/json' -d "$PAYLOAD" >/dev/null &
done
wait

echo "== e2e: verification queries"
echo "-- wallets --"
sqlite3 wallets.db "SELECT id, balance FROM wallets ORDER BY id;"

echo "-- transfers (latest 10) --"
sqlite3 wallets.db "SELECT id, from_wallet_id, to_wallet_id, amount, state, created_at FROM transfers ORDER BY created_at DESC LIMIT 10;"

echo "-- ledger entries (latest 10) --"
sqlite3 wallets.db "SELECT id, wallet_id, transfer_id, type, amount, created_at FROM ledger_entries ORDER BY id DESC LIMIT 10;"

echo "-- ledger counts per transfer (should be 2) --"
sqlite3 wallets.db "SELECT transfer_id, COUNT(*) as cnt FROM ledger_entries GROUP BY transfer_id ORDER BY cnt DESC LIMIT 10;"

echo "== e2e: done"
exit 0
