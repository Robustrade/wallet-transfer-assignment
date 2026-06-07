#!/usr/bin/env bash
#
# Wallet Transfer Service — end-to-end smoke test with plain curl.
#
# Exercises every scenario the assignment cares about against a running server:
#   1. health check
#   2. wallet creation (server-generated ids)
#   3. successful transfer (201, PROCESSED)
#   4. idempotent replay (200, same id, no second debit)
#   5. idempotency-key conflict (409)
#   6. insufficient funds (201, FAILED, no ledger entries)
#   7. reads: get transfer, get wallet, get ledger
#   8. concurrency: N parallel requests sharing ONE idempotency key debit once
#
# Usage:
#   make run                 # start the server (in-memory store) in another shell
#   ./examples/smoke.sh      # or: BASE_URL=http://localhost:8080 ./examples/smoke.sh
#
# Requires: bash, curl, jq.

set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
API="${BASE_URL}/api/v1"
PARALLEL="${PARALLEL:-20}" # concurrent requests for the idempotency burst

# A unique id per run so idempotency keys are always fresh. Without this the
# script could only be run once against a long-lived server (notably Postgres,
# where data persists): re-using a key with new wallets would (correctly) 409.
RUN_ID="$(date +%s)-$$"
key() { printf '%s-%s' "$1" "$RUN_ID"; } # key <name> -> "<name>-<run id>"

# --- tiny output helpers ----------------------------------------------------
green() { printf '\033[32m%s\033[0m\n' "$1"; }
red() { printf '\033[31m%s\033[0m\n' "$1"; }
step() { printf '\n\033[1m== %s ==\033[0m\n' "$1"; }

pass=0
fail=0
check() {
	# check "<description>" "<actual>" "<expected>"
	if [[ "$2" == "$3" ]]; then
		green "  ✓ $1 ($2)"
		pass=$((pass + 1))
	else
		red "  ✗ $1 — expected '$3', got '$2'"
		fail=$((fail + 1))
	fi
}

command -v jq >/dev/null 2>&1 || {
	red "jq is required (brew install jq)"
	exit 1
}

# status CODE METHOD URL [BODY] -> prints HTTP status code
status() {
	local method="$1" url="$2" body="${3:-}"
	if [[ -n "$body" ]]; then
		curl -s -o /dev/null -w '%{http_code}' -X "$method" "$url" \
			-H 'Content-Type: application/json' -d "$body"
	else
		curl -s -o /dev/null -w '%{http_code}' -X "$method" "$url"
	fi
}

# json METHOD URL [BODY] -> prints response body
json() {
	local method="$1" url="$2" body="${3:-}"
	if [[ -n "$body" ]]; then
		curl -s -X "$method" "$url" -H 'Content-Type: application/json' -d "$body"
	else
		curl -s -X "$method" "$url"
	fi
}

# --- 1. health --------------------------------------------------------------
step "Health check"
check "GET /healthz" "$(status GET "${BASE_URL}/healthz")" "200"

# --- 2. wallets -------------------------------------------------------------
step "Create wallets (server-generated ids)"
SRC=$(json POST "${API}/wallets" '{"openingBalance":1000,"currency":"USD"}' | jq -r .id)
DST=$(json POST "${API}/wallets" '{"openingBalance":0,"currency":"USD"}' | jq -r .id)
green "  source wallet: ${SRC}"
green "  dest wallet:   ${DST}"
[[ -n "$SRC" && "$SRC" != "null" ]] || {
	red "wallet creation failed"
	exit 1
}

# --- 3. successful transfer -------------------------------------------------
step "Transfer 100 (first call -> 201 PROCESSED)"
K_MAIN="$(key abc)"
RESP=$(json POST "${API}/transfers" \
	"{\"idempotencyKey\":\"${K_MAIN}\",\"fromWalletId\":\"${SRC}\",\"toWalletId\":\"${DST}\",\"amount\":100}")
TRANSFER_ID=$(echo "$RESP" | jq -r .id)
check "status PROCESSED" "$(echo "$RESP" | jq -r .status)" "PROCESSED"

# --- 4. idempotent replay ---------------------------------------------------
step "Replay SAME request (-> 200 OK, same id, no second debit)"
CODE=$(status POST "${API}/transfers" \
	"{\"idempotencyKey\":\"${K_MAIN}\",\"fromWalletId\":\"${SRC}\",\"toWalletId\":\"${DST}\",\"amount\":100}")
check "HTTP status" "$CODE" "200"
REPLAY_ID=$(json POST "${API}/transfers" \
	"{\"idempotencyKey\":\"${K_MAIN}\",\"fromWalletId\":\"${SRC}\",\"toWalletId\":\"${DST}\",\"amount\":100}" | jq -r .id)
check "same transfer id" "$REPLAY_ID" "$TRANSFER_ID"
check "balance debited once" "$(json GET "${API}/wallets/${SRC}" | jq -r .balance)" "900"

# --- 5. idempotency-key conflict --------------------------------------------
step "Reuse key with DIFFERENT params (-> 409 Conflict)"
check "HTTP status" \
	"$(status POST "${API}/transfers" \
		"{\"idempotencyKey\":\"${K_MAIN}\",\"fromWalletId\":\"${SRC}\",\"toWalletId\":\"${DST}\",\"amount\":500}")" \
	"409"

# --- 6. insufficient funds --------------------------------------------------
step "Overdraw (-> 201 Created, status FAILED, no ledger entries)"
RESP=$(json POST "${API}/transfers" \
	"{\"idempotencyKey\":\"$(key overdraw)\",\"fromWalletId\":\"${DST}\",\"toWalletId\":\"${SRC}\",\"amount\":999999}")
check "status FAILED" "$(echo "$RESP" | jq -r .status)" "FAILED"
check "reason INSUFFICIENT_FUNDS" "$(echo "$RESP" | jq -r .failureReason)" "INSUFFICIENT_FUNDS"
check "no ledger entry on dest" "$(json GET "${API}/wallets/${DST}/ledger" | jq '.entries | length')" "1"

# --- 7. reads ---------------------------------------------------------------
step "Reads (get transfer, wallet, ledger)"
check "GET transfer" "$(status GET "${API}/transfers/${TRANSFER_ID}")" "200"
check "GET wallet" "$(status GET "${API}/wallets/${SRC}")" "200"
check "GET unknown transfer -> 404" "$(status GET "${API}/transfers/does-not-exist")" "404"
echo "  source ledger (newest first):"
json GET "${API}/wallets/${SRC}/ledger" | jq -c '.entries[]' | sed 's/^/    /'

# --- 8. concurrency: one key, many parallel requests ------------------------
step "Concurrency: ${PARALLEL} parallel requests sharing ONE idempotency key"
CSRC=$(json POST "${API}/wallets" '{"openingBalance":1000,"currency":"USD"}' | jq -r .id)
CDST=$(json POST "${API}/wallets" '{"openingBalance":0,"currency":"USD"}' | jq -r .id)
BODY="{\"idempotencyKey\":\"$(key burst)\",\"fromWalletId\":\"${CSRC}\",\"toWalletId\":\"${CDST}\",\"amount\":100}"

seq "$PARALLEL" | xargs -P "$PARALLEL" -I{} \
	curl -s -o /dev/null -X POST "${API}/transfers" -H 'Content-Type: application/json' -d "$BODY"

# Despite the burst, exactly one debit of 100 must have landed.
check "source debited exactly once" "$(json GET "${API}/wallets/${CSRC}" | jq -r .balance)" "900"
check "dest credited exactly once" "$(json GET "${API}/wallets/${CDST}" | jq -r .balance)" "100"
check "exactly one ledger entry" "$(json GET "${API}/wallets/${CSRC}/ledger" | jq '.entries | length')" "1"

# --- summary ----------------------------------------------------------------
step "Summary"
green "  passed: ${pass}"
if [[ "$fail" -gt 0 ]]; then
	red "  failed: ${fail}"
	exit 1
fi
green "  all scenarios passed ✅"
