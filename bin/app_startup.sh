#!/usr/bin/env bash
#
# app_startup.sh — build, launch and smoke-test the Wallet Transfer Service.
#
# Runs the exact sequence used during development:
#   1. check prerequisites (Go, PostgreSQL)
#   2. build the server binary
#   3. start the server (embedded migrations apply on startup)
#   4. wait for readiness
#   5. seed development wallet fixtures
#   6. exercise every endpoint and print the result of each call
#
# Everything printed here is also written to logs/app_startup_<timestamp>.log.
#
# Usage:
#   ./bin/app_startup.sh                 # start + smoke test, leave server running
#   ./bin/app_startup.sh --stop          # stop a server started by this script
#   KEEP_RUNNING=0 ./bin/app_startup.sh  # stop the server again once tests pass
#
# Configuration is resolved in this order (first match wins):
#   1. WALLET_DATABASE_URL / WALLET_HTTP_ADDR / WALLET_LOG_LEVEL   (explicit overrides)
#   2. .env.local  (gitignored personal overrides)
#   3. .env        (committed local-dev defaults)
#   4. built-in defaults
# A bare, unrelated DATABASE_URL already in your shell is deliberately ignored.
#
#   KEEP_RUNNING   default 1 (0 = shut the server down once tests pass)

set -euo pipefail

# --------------------------------------------------------------------------- #
# configuration
# --------------------------------------------------------------------------- #
# this script lives in bin/ — the project root is its parent
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

DEFAULT_DATABASE_URL="postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable"

# load .env (committed defaults) then .env.local (personal overrides)
for envfile in .env .env.local; do
  if [[ -f "$envfile" ]]; then
    set -a
    # shellcheck disable=SC1090,SC1091
    source "./$envfile"
    set +a
  fi
done

export DATABASE_URL="${WALLET_DATABASE_URL:-${DATABASE_URL:-$DEFAULT_DATABASE_URL}}"
export HTTP_ADDR="${WALLET_HTTP_ADDR:-${HTTP_ADDR:-:8080}}"
export LOG_LEVEL="${WALLET_LOG_LEVEL:-${LOG_LEVEL:-info}}"
KEEP_RUNNING="${KEEP_RUNNING:-1}"

PORT="${HTTP_ADDR##*:}"
BASE_URL="http://localhost:${PORT}"

RUN_DIR="$ROOT_DIR/.run"
LOG_DIR="$ROOT_DIR/logs"
mkdir -p "$RUN_DIR" "$LOG_DIR"

PID_FILE="$RUN_DIR/server.pid"
SERVER_LOG="$LOG_DIR/server.log"
RUN_LOG="$LOG_DIR/app_startup_$(date +%Y%m%d_%H%M%S).log"

# mirror all output to the run log
exec > >(tee -a "$RUN_LOG") 2>&1

step()  { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
info()  { printf '    %s\n' "$*"; }
ok()    { printf '    \033[32mOK\033[0m  %s\n' "$*"; }
die()   { printf '    \033[31mFAIL\033[0m %s\n' "$*"; exit 1; }

# --------------------------------------------------------------------------- #
# --stop
# --------------------------------------------------------------------------- #
stop_server() {
  if [[ -f "$PID_FILE" ]] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    local pid; pid="$(cat "$PID_FILE")"
    info "stopping server (pid $pid)"
    kill -INT "$pid" 2>/dev/null || true
    for _ in $(seq 1 20); do kill -0 "$pid" 2>/dev/null || break; sleep 0.2; done
    kill -KILL "$pid" 2>/dev/null || true
    rm -f "$PID_FILE"
    ok "server stopped"
  else
    info "no running server recorded in $PID_FILE"
  fi
}

if [[ "${1:-}" == "--stop" ]]; then
  step "Stopping Wallet Transfer Service"
  stop_server
  exit 0
fi

# --------------------------------------------------------------------------- #
# 1. prerequisites
# --------------------------------------------------------------------------- #
step "1. Checking prerequisites"

command -v go >/dev/null || die "go not found on PATH"
ok "go $(go version | awk '{print $3}')"

DB_HOST_PORT="$(printf '%s' "$DATABASE_URL" | sed -E 's#.*@([^/?]+).*#\1#')"
DB_HOST="${DB_HOST_PORT%%:*}"
DB_PORT="${DB_HOST_PORT##*:}"
if (exec 3<>"/dev/tcp/${DB_HOST}/${DB_PORT}") 2>/dev/null; then
  ok "PostgreSQL reachable at ${DB_HOST}:${DB_PORT}"
else
  die "PostgreSQL not reachable at ${DB_HOST}:${DB_PORT} — start it with 'make db-up'"
fi

# --------------------------------------------------------------------------- #
# 2. build
# --------------------------------------------------------------------------- #
step "2. Building"
go build -o bin/server ./cmd/server
ok "bin/server ($(du -h bin/server | cut -f1))"

# --------------------------------------------------------------------------- #
# 3. start server
# --------------------------------------------------------------------------- #
step "3. Starting server on ${HTTP_ADDR}"

if [[ -f "$PID_FILE" ]] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
  info "server already running (pid $(cat "$PID_FILE")); restarting"
  stop_server
fi

: > "$SERVER_LOG"
nohup ./bin/server >> "$SERVER_LOG" 2>&1 &
SERVER_PID=$!
echo "$SERVER_PID" > "$PID_FILE"
info "pid $SERVER_PID, log $SERVER_LOG"

# --------------------------------------------------------------------------- #
# 4. wait for readiness
# --------------------------------------------------------------------------- #
step "4. Waiting for readiness"
READY=0
for _ in $(seq 1 40); do
  if curl -sf "${BASE_URL}/readyz" >/dev/null 2>&1; then READY=1; break; fi
  kill -0 "$SERVER_PID" 2>/dev/null || { cat "$SERVER_LOG"; die "server exited during startup"; }
  sleep 0.25
done
[[ "$READY" == 1 ]] || { cat "$SERVER_LOG"; die "server did not become ready"; }
ok "server is ready"
info "startup log:"
sed 's/^/      /' "$SERVER_LOG"

# --------------------------------------------------------------------------- #
# 5. seed development wallets
# --------------------------------------------------------------------------- #
step "5. Seeding development wallets"
go run ./cmd/seeddev | sed 's/^/    /'

# --------------------------------------------------------------------------- #
# 6. smoke tests
# --------------------------------------------------------------------------- #
step "6. Smoke tests"

# call METHOD PATH [json-body] -> prints "HTTP <code> <body>"
call() {
  local method="$1" path="$2" body="${3:-}"
  local args=(-s -o /dev/null -w '%{http_code}' -X "$method" "${BASE_URL}${path}")
  local full=(-s -X "$method" "${BASE_URL}${path}")
  if [[ -n "$body" ]]; then
    args+=(-H 'content-type: application/json' -d "$body")
    full+=(-H 'content-type: application/json' -d "$body")
  fi
  local code; code="$(curl "${args[@]}")"
  local payload; payload="$(curl "${full[@]}")"
  printf 'HTTP %s  %s\n' "$code" "$payload"
}

expect() { # expect <label> <expected-code> <actual "HTTP nnn ...">
  local label="$1" want="$2" got="$3"
  local code="${got#HTTP }"; code="${code%% *}"
  if [[ "$code" == "$want" ]]; then ok "$label -> $got"; else die "$label expected $want, got: $got"; fi
}

# Fresh idempotency keys each run so re-runs still see a 201 on first create.
RUN_ID="$(date +%s)-$RANDOM"
KEY_OK="startup-${RUN_ID}"
KEY_POOR="startup-poor-${RUN_ID}"
KEY_SELF="startup-self-${RUN_ID}"

expect "health"                 200 "$(call GET  /healthz)"
expect "readiness"              200 "$(call GET  /readyz)"
expect "create transfer"        201 "$(call POST /transfers "{\"idempotencyKey\":\"$KEY_OK\",\"fromWalletId\":\"wallet_1\",\"toWalletId\":\"wallet_2\",\"amount\":2500}")"
expect "idempotent replay"      200 "$(call POST /transfers "{\"idempotencyKey\":\"$KEY_OK\",\"fromWalletId\":\"wallet_1\",\"toWalletId\":\"wallet_2\",\"amount\":2500}")"
expect "key reuse conflict"     409 "$(call POST /transfers "{\"idempotencyKey\":\"$KEY_OK\",\"fromWalletId\":\"wallet_1\",\"toWalletId\":\"wallet_2\",\"amount\":9999}")"
expect "insufficient funds"     422 "$(call POST /transfers "{\"idempotencyKey\":\"$KEY_POOR\",\"fromWalletId\":\"wallet_2\",\"toWalletId\":\"wallet_1\",\"amount\":100000000}")"
expect "self transfer rejected" 400 "$(call POST /transfers "{\"idempotencyKey\":\"$KEY_SELF\",\"fromWalletId\":\"wallet_1\",\"toWalletId\":\"wallet_1\",\"amount\":1}")"
expect "unknown wallet"         404 "$(call GET  /wallets/does_not_exist/balance)"
expect "wallet_1 balance"       200 "$(call GET  /wallets/wallet_1/balance)"
expect "wallet_2 balance"       200 "$(call GET  /wallets/wallet_2/balance)"

# --------------------------------------------------------------------------- #
# summary
# --------------------------------------------------------------------------- #
step "Done"
info "run log     : $RUN_LOG"
info "server log  : $SERVER_LOG"
info "server pid  : $SERVER_PID  ($BASE_URL)"

if [[ "$KEEP_RUNNING" == "1" ]]; then
  info "server left running — stop it with: ./app_startup.sh --stop"
else
  stop_server
fi
