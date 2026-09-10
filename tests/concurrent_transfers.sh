#!/bin/bash

URL="http://localhost:8080/transfers"

FROM="11111111-1111-1111-1111-111111111111"
TO="22222222-2222-2222-2222-222222222222"

REQUESTS=10
AMOUNT=100

echo "Sending $REQUESTS concurrent transfer requests..."

for i in $(seq 1 $REQUESTS); do
  (
    echo "----- Request $i -----"

    curl -s -w "\nHTTP_STATUS: %{http_code}\n" \
      -X POST "$URL" \
      -H "Content-Type: application/json" \
      -d "{
        \"idempotencyKey\": \"concurrent-$i\",
        \"fromWalletId\": \"$FROM\",
        \"toWalletId\": \"$TO\",
        \"amount\": $AMOUNT
      }"

    echo
  ) &
done

wait

echo "All concurrent requests completed."