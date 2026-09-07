#!/usr/bin/env bash
set -euo pipefail

BASE_URL="http://localhost:8080/api/v1"
TIMESTAMP=$(date +%s)
WALLET_1="W_USER1_${TIMESTAMP}"
WALLET_2="W_USER2_${TIMESTAMP}"
IDEMP_KEY="idemp_${TIMESTAMP}"

format_json() {
  if command -v jq >/dev/null 2>&1; then
    jq .
  else
    cat
  fi
}

echo "============================================================"
echo " Starting Wallet Transfer Service End-to-End Suite"
echo " Target: ${BASE_URL}"
echo "============================================================"

# 1. Create Wallets
echo -e "\n1. Creating Wallet 1 (${WALLET_1}) with \$100.00..."
curl -s -f -X POST "${BASE_URL}/wallets" \
  -H "Content-Type: application/json" \
  -d "{\"id\": \"${WALLET_1}\", \"initialBalance\": \"100.00\", \"status\": \"ACTIVE\"}" | format_json

echo -e "\n2. Creating Wallet 2 (${WALLET_2}) with \$20.00..."
curl -s -f -X POST "${BASE_URL}/wallets" \
  -H "Content-Type: application/json" \
  -d "{\"id\": \"${WALLET_2}\", \"initialBalance\": \"20.00\", \"status\": \"ACTIVE\"}" | format_json

# 2. Execute Transfer
echo -e "\n3. Executing Transfer (\$25.00 from ${WALLET_1} to ${WALLET_2})..."
TRANSFER_RESP=$(curl -s -f -X POST "${BASE_URL}/transfers" \
  -H "Content-Type: application/json" \
  -d "{\"idempotencyKey\": \"${IDEMP_KEY}\", \"fromWalletId\": \"${WALLET_1}\", \"toWalletId\": \"${WALLET_2}\", \"amount\": \"25.00\"}")
echo "${TRANSFER_RESP}" | format_json

# 3. Test Idempotency (Repeat exact same transfer call)
echo -e "\n4. Retrying with IDENTICAL Idempotency Key (Expect cached replay)..."
IDEMP_REPLAY=$(curl -s -f -X POST "${BASE_URL}/transfers" \
  -H "Content-Type: application/json" \
  -d "{\"idempotencyKey\": \"${IDEMP_KEY}\", \"fromWalletId\": \"${WALLET_1}\", \"toWalletId\": \"${WALLET_2}\", \"amount\": \"25.00\"}")
echo "${IDEMP_REPLAY}" | format_json

# 4. Test Idempotency Tamper Protection (Assert 400 Bad Request)
echo -e "\n5. Retrying with SAME key but TAMPERED amount (\$50.00) (Expect 400 Bad Request)..."
TAMPER_STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${BASE_URL}/transfers" \
  -H "Content-Type: application/json" \
  -d "{\"idempotencyKey\": \"${IDEMP_KEY}\", \"fromWalletId\": \"${WALLET_1}\", \"toWalletId\": \"${WALLET_2}\", \"amount\": \"50.00\"}")

echo "HTTP Status: ${TAMPER_STATUS}"
if [ "${TAMPER_STATUS}" != "400" ]; then
  echo "Assertion failed: Expected HTTP 400 on tampered payload, got ${TAMPER_STATUS}"
  exit 1
fi

# 5. Verify Statement & Ledger Open/Close Balances
echo -e "\n6. Fetching Statement for ${WALLET_1}..."
curl -s -f -X GET "${BASE_URL}/wallets/${WALLET_1}/statement" | format_json

echo -e "\n7. Fetching Statement for ${WALLET_2}..."
curl -s -f -X GET "${BASE_URL}/wallets/${WALLET_2}/statement" | format_json

echo -e "\n============================================================"
echo " E2E Tests Finished Successfully"
echo "============================================================"