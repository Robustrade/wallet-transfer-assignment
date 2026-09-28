#!/usr/bin/env bash
# Walks through every transfer outcome against a running server:
#   make run      # in one terminal
#   make demo     # in another
#
# Idempotency keys are prefixed with a per-run id, so the demo can be re-run
# against the same database. Requires only bash and curl.
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
RUN="demo-$(date +%s)"

if ! curl -fsS "$BASE_URL/healthz" >/dev/null 2>&1; then
  echo "Server is not reachable at $BASE_URL. Start it with: make run" >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# post TITLE JSON: sends a transfer and prints the status line, the replay
# header (if any) and the response body.
post() {
  local title="$1" body="$2"
  echo "== $title"
  echo "   > $body"
  curl -sS -D "$tmp/headers" -o "$tmp/body" -X POST "$BASE_URL/transfers" \
    -H 'Content-Type: application/json' -d "$body"
  tr -d '\r' <"$tmp/headers" | grep -E '^HTTP/|^Idempotent-Replayed' | sed 's/^/   < /'
  echo "   < $(cat "$tmp/body")"
  echo
}

# balance WALLET: prints just the balance number.
balance() {
  curl -sS "$BASE_URL/wallets/$1" | sed -E 's/.*"balance":([0-9]+).*/\1/'
}

balances() {
  echo "== Balances"
  for w in wallet_1 wallet_2 wallet_3 wallet_usd; do
    printf '   %-10s ' "$w"
    curl -sS "$BASE_URL/wallets/$w"
    echo
  done
  echo
}

balances

post "1. Normal transfer (expect 201 PROCESSED)" \
  "{\"idempotencyKey\":\"$RUN-1\",\"fromWalletId\":\"wallet_1\",\"toWalletId\":\"wallet_2\",\"amount\":1000}"

post "2. Same request again (expect 201 + Idempotent-Replayed: true, same id)" \
  "{\"idempotencyKey\":\"$RUN-1\",\"fromWalletId\":\"wallet_1\",\"toWalletId\":\"wallet_2\",\"amount\":1000}"

post "3. Same key, different amount (expect 409 IDEMPOTENCY_KEY_REUSED)" \
  "{\"idempotencyKey\":\"$RUN-1\",\"fromWalletId\":\"wallet_1\",\"toWalletId\":\"wallet_2\",\"amount\":9999}"

post "4. Insufficient funds from the empty wallet_3 (expect 422 FAILED)" \
  "{\"idempotencyKey\":\"$RUN-4\",\"fromWalletId\":\"wallet_3\",\"toWalletId\":\"wallet_1\",\"amount\":500}"

post "5. Currency mismatch SGD -> USD (expect 422 FAILED)" \
  "{\"idempotencyKey\":\"$RUN-5\",\"fromWalletId\":\"wallet_1\",\"toWalletId\":\"wallet_usd\",\"amount\":100}"

post "6. Unknown wallet (expect 404 WALLET_NOT_FOUND, nothing stored)" \
  "{\"idempotencyKey\":\"$RUN-6\",\"fromWalletId\":\"wallet_1\",\"toWalletId\":\"wallet_404\",\"amount\":100}"

post "7. Float amount (expect 400 VALIDATION_ERROR)" \
  "{\"idempotencyKey\":\"$RUN-7\",\"fromWalletId\":\"wallet_1\",\"toWalletId\":\"wallet_2\",\"amount\":10.5}"

echo "== 8. Parallel burst"

echo "   20 concurrent requests with the SAME key: one executes, 19 replay"
for i in $(seq 1 20); do
  curl -sS -o /dev/null -D "$tmp/same-$i" -X POST "$BASE_URL/transfers" \
    -H 'Content-Type: application/json' \
    -d "{\"idempotencyKey\":\"$RUN-burst\",\"fromWalletId\":\"wallet_2\",\"toWalletId\":\"wallet_3\",\"amount\":10}" &
done
wait
echo "   replayed responses: $(grep -il '^Idempotent-Replayed: true' "$tmp"/same-* | wc -l | tr -d ' ') of 20"

# Fund wallet_3 so that only some of the burst below can succeed.
curl -sS -o /dev/null -X POST "$BASE_URL/transfers" -H 'Content-Type: application/json' \
  -d "{\"idempotencyKey\":\"$RUN-fund\",\"fromWalletId\":\"wallet_1\",\"toWalletId\":\"wallet_3\",\"amount\":5000}"
before="$(balance wallet_3)"
echo "   wallet_3 now holds $before; firing 30 concurrent transfers of 1000 out of it"
for i in $(seq 1 30); do
  curl -sS -o /dev/null -w '%{http_code}\n' -X POST "$BASE_URL/transfers" \
    -H 'Content-Type: application/json' \
    -d "{\"idempotencyKey\":\"$RUN-drain-$i\",\"fromWalletId\":\"wallet_3\",\"toWalletId\":\"wallet_1\",\"amount\":1000}" \
    >>"$tmp/codes" &
done
wait
echo "   status codes: $(sort "$tmp/codes" | uniq -c | awk '{printf "%s x %s  ", $1, $2}')"
echo "   expected exactly $((before / 1000)) x 201; wallet_3 is left with $(balance wallet_3) (never negative)"
echo

balances

echo "== Latest 3 transfers involving wallet_3"
curl -sS "$BASE_URL/wallets/wallet_3/transfers?limit=3"
echo
