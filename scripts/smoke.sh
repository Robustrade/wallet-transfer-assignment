#!/usr/bin/env bash
#
# End-to-end smoke test walkthrough of the wallet transfer API.
#
# Usage:  make smoke   (or: BASE_URL=http://localhost:8080 ./scripts/smoke.sh)

set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
FAILURES=0

bold()  { printf '\n\033[1m%s\033[0m\n' "$1"; }
pass()  { printf '  \033[32m✓\033[0m %s\n' "$1"; }
fail()  { printf '  \033[31m✗\033[0m %s\n' "$1"; FAILURES=$((FAILURES + 1)); }

expect() {
  if [[ "$2" == "$3" ]]; then
    pass "$1"
  else
    fail "$1 (expected '$3', got '$2')"
  fi
}

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
BODY_FILE="$TMP_DIR/body.json"

post_transfer() {
  curl -sS -o "$BODY_FILE" -w '%{http_code}' \
    -X POST "$BASE_URL/transfers" \
    -H 'Content-Type: application/json' \
    -d "{\"idempotencyKey\":\"$1\",\"fromWalletId\":\"$2\",\"toWalletId\":\"$3\",\"amount\":$4}"
}

json_field() {
  grep -o "\"$1\":[^,}]*" "$BODY_FILE" | head -1 | cut -d: -f2 | tr -d ' "'
}

balance_of() {
  curl -sS "$BASE_URL/wallets/$1" | grep -o '"balance":[0-9-]*' | cut -d: -f2
}

# Seed demo wallets if possible
go run ./cmd/seed >/dev/null 2>&1 || true

bold "Wallet Transfer Smoke Test against $BASE_URL"

# ---------------------------------------------------------------------------
bold "0. Health & Readiness checks"
READY=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE_URL/readyz" || echo "000")
if [[ "$READY" != "200" ]]; then
  fail "API is not ready (GET /readyz returned $READY) — make sure server is running"
  exit 1
fi
pass "GET /readyz -> 200 OK (database connected)"

HEALTH=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE_URL/healthz" || echo "000")
expect "GET /healthz -> 200 OK (liveness probe)" "$HEALTH" "200"

OPENING_FROM=$(balance_of wallet_1)
OPENING_TO=$(balance_of wallet_2)
printf '  wallet_1 opening balance: %s, wallet_2: %s (in minor units / paise)\n' "$OPENING_FROM" "$OPENING_TO"

KEY="smoke-$(date +%s)-$$"
AMOUNT=2500

# ---------------------------------------------------------------------------
bold "1. Valid transfer moves funds and creates balanced ledger entries"
STATUS=$(post_transfer "$KEY" wallet_1 wallet_2 "$AMOUNT")
expect "POST /transfers -> 201 Created" "$STATUS" "201"

TRANSFER_ID=$(json_field transferId)
FIRST_BODY=$(cat "$BODY_FILE")
expect "transfer status is PROCESSED" "$(json_field status)" "PROCESSED"
expect "wallet_1 debited by $AMOUNT" "$(balance_of wallet_1)" "$((OPENING_FROM - AMOUNT))"
expect "wallet_2 credited by $AMOUNT" "$(balance_of wallet_2)" "$((OPENING_TO + AMOUNT))"

ENTRIES=$(curl -sS "$BASE_URL/transfers/$TRANSFER_ID")
if echo "$ENTRIES" | grep -q '"type":"DEBIT"' && echo "$ENTRIES" | grep -q '"type":"CREDIT"'; then
  pass "transfer detail contains balanced DEBIT and CREDIT ledger entries"
else
  fail "expected transfer detail to contain balanced DEBIT and CREDIT entries"
fi

# ---------------------------------------------------------------------------
bold "2. Idempotent replay: identical request returns cached result with replay header"
REPLAY_CODE=$(curl -sS -D "$TMP_DIR/headers.txt" -o "$BODY_FILE" -w '%{http_code}' \
  -X POST "$BASE_URL/transfers" \
  -H 'Content-Type: application/json' \
  -d "{\"idempotencyKey\":\"$KEY\",\"fromWalletId\":\"wallet_1\",\"toWalletId\":\"wallet_2\",\"amount\":$AMOUNT}")

expect "replay request returns 200 OK" "$REPLAY_CODE" "200"

if grep -qi "Idempotent-Replay: true" "$TMP_DIR/headers.txt"; then
  pass "Idempotent-Replay header is 'true'"
else
  fail "expected Idempotent-Replay: true header"
fi

expect "wallet_1 was NOT debited again" "$(balance_of wallet_1)" "$((OPENING_FROM - AMOUNT))"
expect "wallet_2 was NOT credited again" "$(balance_of wallet_2)" "$((OPENING_TO + AMOUNT))"

# ---------------------------------------------------------------------------
bold "3. Idempotency conflict: reusing key with altered payload returns 409 Conflict"
STATUS=$(post_transfer "$KEY" wallet_1 wallet_2 9999)
expect "altered payload on same key returns 409 Conflict" "$STATUS" "409"
expect "error code is IDEMPOTENCY_KEY_CONFLICT" "$(json_field code)" "IDEMPOTENCY_KEY_CONFLICT"
expect "balances remain unaffected" "$(balance_of wallet_1)" "$((OPENING_FROM - AMOUNT))"

# ---------------------------------------------------------------------------
bold "4. Overdraft protection: transfers exceeding balance return 422 Unprocessable Entity"
CURRENT_BALANCE=$(balance_of wallet_1)
OVERDRAFT_AMOUNT=$((CURRENT_BALANCE + 5000))
STATUS=$(post_transfer "overdraft-$KEY" wallet_1 wallet_2 "$OVERDRAFT_AMOUNT")
expect "overdraft attempt returns 422 Unprocessable Entity" "$STATUS" "422"
expect "error code is INSUFFICIENT_FUNDS" "$(json_field code)" "INSUFFICIENT_FUNDS"
expect "balance remained intact" "$(balance_of wallet_1)" "$CURRENT_BALANCE"

# ---------------------------------------------------------------------------
bold "5. Validation rules: self-transfer and cross-currency rejection"
STATUS=$(post_transfer "self-$KEY" wallet_1 wallet_1 1000)
expect "self-transfer returns 400 Bad Request" "$STATUS" "400"
expect "error code is VALIDATION_ERROR" "$(json_field code)" "VALIDATION_ERROR"

STATUS=$(post_transfer "curr-$KEY" wallet_1 wallet_usd 1000)
expect "cross-currency transfer returns 422 Unprocessable Entity" "$STATUS" "422"
expect "error code is CURRENCY_MISMATCH" "$(json_field code)" "CURRENCY_MISMATCH"

# ---------------------------------------------------------------------------
bold "6. Wallet queries & operational endpoints"
WALLET_CODE=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE_URL/wallets/wallet_1")
expect "GET /wallets/:id returns 200 OK" "$WALLET_CODE" "200"

HISTORY_CODE=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE_URL/wallets/wallet_1/transfers?limit=5")
expect "GET /wallets/:id/transfers returns 200 OK" "$HISTORY_CODE" "200"

METRICS_BODY=$(curl -sS "$BASE_URL/metrics")
if echo "$METRICS_BODY" | grep -q "transfers_total"; then
  pass "GET /metrics exposes transfers_total Prometheus metric"
else
  fail "GET /metrics missing transfers_total metric"
fi

# ---------------------------------------------------------------------------
bold "Smoke Test Summary"
if [[ $FAILURES -eq 0 ]]; then
  printf '\033[32mAll smoke tests passed successfully!\033[0m\n\n'
else
  printf '\033[31m%d test(s) failed.\033[0m\n\n' "$FAILURES"
  exit 1
fi
