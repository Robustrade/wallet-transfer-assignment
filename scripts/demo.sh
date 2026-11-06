#!/usr/bin/env bash
# Walks through the API's guarantees against a running stack (`make up` first).
# Every step asserts the expected status code, so this doubles as a smoke test.
# The demo is balance-neutral: it transfers the money back at the end.
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:${APP_HOST_PORT:-8080}}"
RUN="$(date +%s)-$$" # unique idempotency keys per run
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

bold() { printf '\n\033[1m%s\033[0m\n' "$*"; }

# request METHOD PATH [BODY] -> prints status, selected headers and body; sets $STATUS
request() {
  local method=$1 path=$2 body=${3:-}
  local args=(-s -o "$TMP/body" -D "$TMP/headers" -w '%{http_code}' -X "$method" "$BASE_URL$path")
  [[ -n "$body" ]] && args+=(-H 'Content-Type: application/json' -d "$body")
  STATUS=$(curl "${args[@]}")
  echo "→ $method $path ${body:+$body}"
  echo "← $STATUS $(grep -i '^idempotent-replayed' "$TMP/headers" | tr -d '\r' || true)"
  echo "  $(cat "$TMP/body")"
}

expect() {
  if [[ "$STATUS" != "$1" ]]; then
    echo "✗ expected HTTP $1, got $STATUS" >&2
    exit 1
  fi
  echo "✓ $2"
}

transfer() { # key from to amount
  printf '{"idempotencyKey":"%s","fromWalletId":"%s","toWalletId":"%s","amount":%s}' "$1" "$2" "$3" "$4"
}

bold "0. Starting balances"
request GET /wallets/wallet_1; expect 200 "wallet_1 found"
request GET /wallets/wallet_2; expect 200 "wallet_2 found"

bold "1. Transfer 100 from wallet_1 to wallet_2"
request POST /transfers "$(transfer "demo-$RUN-1" wallet_1 wallet_2 100)"
expect 201 "processed"

bold "2. Client retries the same request (e.g. the response was lost)"
request POST /transfers "$(transfer "demo-$RUN-1" wallet_1 wallet_2 100)"
expect 201 "replayed: same transfer id, Idempotent-Replayed: true, no money moved"

bold "3. Same key, different amount"
request POST /transfers "$(transfer "demo-$RUN-1" wallet_1 wallet_2 999)"
expect 422 "rejected: IDEMPOTENCY_KEY_REUSED"

bold "4. Spend more than wallet_3 holds"
balance3=$(curl -s "$BASE_URL/wallets/wallet_3" | grep -o '"balance":[0-9]*' | cut -d: -f2)
request POST /transfers "$(transfer "demo-$RUN-2" wallet_3 wallet_1 $((balance3 + 1)))"
expect 422 "recorded as FAILED: INSUFFICIENT_FUNDS (balance $balance3)"

bold "5. Malformed amount"
request POST /transfers '{"idempotencyKey":"demo-x","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":10.5}'
expect 400 "rejected: VALIDATION_ERROR"

bold "6. 20 concurrent duplicates of one new request"
for i in $(seq 1 20); do
  curl -s -o /dev/null -D "$TMP/h$i" -X POST "$BASE_URL/transfers" \
    -H 'Content-Type: application/json' -d "$(transfer "demo-$RUN-3" wallet_2 wallet_1 100)" &
done
wait
replays=$(cat "$TMP"/h* | grep -ci '^idempotent-replayed: true' || true)
echo "  originals: $((20 - replays)), replays: $replays"
[[ "$replays" == "19" ]] || { echo "✗ expected exactly one original execution" >&2; exit 1; }
echo "✓ executed exactly once (this also returns step 1's 100 to wallet_1)"

bold "7. Final balances (unchanged overall: the demo is balance-neutral)"
request GET /wallets/wallet_1; expect 200 "wallet_1"
request GET /wallets/wallet_2; expect 200 "wallet_2"

bold "All checks passed."
