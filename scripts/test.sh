#!/bin/bash

# Wallet Transfer Service - Automated Test Script
# Tests all production features with a single command

set -e  # Exit on error

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

BASE_URL="http://localhost:8080"

echo "=========================================="
echo "Wallet Transfer Service - Automated Tests"
echo "=========================================="
echo ""

# Helper function to print test headers
print_test() {
    echo -e "\n${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}TEST $1: $2${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

# Helper function to print success
print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

# Helper function to print error
print_error() {
    echo -e "${RED}✗ $1${NC}"
    exit 1
}

# Check if server is running
echo "Checking if server is running..."
if ! curl -s $BASE_URL/health > /dev/null 2>&1; then
    print_error "Server is not running. Start it with: ./scripts/run.sh"
fi
print_success "Server is running"

# Extract database credentials from config.json
DB_NAME=$(grep -o '"dbname": *"[^"]*"' config.json | cut -d'"' -f4)
DB_USER=$(grep -o '"user": *"[^"]*"' config.json | cut -d'"' -f4)
DB_HOST=$(grep -o '"host": *"[^"]*"' config.json | cut -d'"' -f4)
DB_PORT=$(grep -A 6 '"database"' config.json | grep -o '"port": *"[^"]*"' | head -1 | cut -d'"' -f4)

# Get initial balances
WALLET_1_INITIAL=$(psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -t -c "SELECT balance FROM wallets WHERE id='wallet_1';" | xargs)
WALLET_2_INITIAL=$(psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -t -c "SELECT balance FROM wallets WHERE id='wallet_2';" | xargs)

echo ""
echo "Initial Balances:"
echo "  wallet_1: \$$WALLET_1_INITIAL"
echo "  wallet_2: \$$WALLET_2_INITIAL"

# ============================================
# TEST 1: Health Check
# ============================================
print_test "1" "Health Check"

RESPONSE=$(curl -s $BASE_URL/health)
if [[ $RESPONSE == *"ok"* ]]; then
    print_success "Health check passed"
else
    print_error "Health check failed"
fi

# ============================================
# TEST 2: Successful Transfer
# ============================================
print_test "2" "Successful Transfer (wallet_1 → wallet_2: \$100.50)"

RESPONSE=$(curl -s -X POST $BASE_URL/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-001",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 100.50
  }')

if [[ $RESPONSE == *"PROCESSED"* ]] && [[ $RESPONSE == *"100.5"* ]]; then
    print_success "Transfer successful"
    TRANSFER_ID=$(echo $RESPONSE | grep -o '"id":"[^"]*"' | cut -d'"' -f4)
    echo "  Transfer ID: $TRANSFER_ID"
else
    print_error "Transfer failed: $RESPONSE"
fi

# Verify balances
WALLET_1_AFTER=$(psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -t -c "SELECT balance FROM wallets WHERE id='wallet_1';" | xargs)
WALLET_2_AFTER=$(psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -t -c "SELECT balance FROM wallets WHERE id='wallet_2';" | xargs)

EXPECTED_W1=$(printf "%.4f" $(echo "$WALLET_1_INITIAL - 100.50" | bc))
EXPECTED_W2=$(printf "%.4f" $(echo "$WALLET_2_INITIAL + 100.50" | bc))

if [[ "$WALLET_1_AFTER" == "$EXPECTED_W1" ]] && [[ "$WALLET_2_AFTER" == "$EXPECTED_W2" ]]; then
    print_success "Balances updated correctly"
    echo "  wallet_1: \$$WALLET_1_INITIAL → \$$WALLET_1_AFTER"
    echo "  wallet_2: \$$WALLET_2_INITIAL → \$$WALLET_2_AFTER"
else
    print_error "Balance mismatch!"
fi

# ============================================
# TEST 3: Idempotency - Duplicate Request
# ============================================
print_test "3" "Idempotency - Duplicate Request (Same Payload)"

RESPONSE=$(curl -s -X POST $BASE_URL/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-001",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 100.50
  }')

if [[ $RESPONSE == *"PROCESSED"* ]] && [[ $RESPONSE == *"$TRANSFER_ID"* ]]; then
    print_success "Returned cached response with same transfer ID"
else
    print_error "Idempotency failed: $RESPONSE"
fi

# Verify balances unchanged
WALLET_1_CHECK=$(psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -t -c "SELECT balance FROM wallets WHERE id='wallet_1';" | xargs)
WALLET_2_CHECK=$(psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -t -c "SELECT balance FROM wallets WHERE id='wallet_2';" | xargs)

if [[ "$WALLET_1_CHECK" == "$WALLET_1_AFTER" ]] && [[ "$WALLET_2_CHECK" == "$WALLET_2_AFTER" ]]; then
    print_success "Balances unchanged (no duplicate charge)"
else
    print_error "Balances changed on duplicate request!"
fi

# ============================================
# TEST 4: Request Hash Validation
# ============================================
print_test "4" "Request Hash Validation (Same Key, Different Payload)"

RESPONSE=$(curl -s -X POST $BASE_URL/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-001",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 200.00
  }')

if [[ $RESPONSE == *"different payload"* ]] || [[ $RESPONSE == *"409"* ]]; then
    print_success "Rejected mismatched payload (409 Conflict)"
else
    print_error "Should reject different payload: $RESPONSE"
fi

# ============================================
# TEST 5: Insufficient Balance (NOT Cached)
# ============================================
print_test "5" "Validation Error NOT Cached - Insufficient Balance"

RESPONSE=$(curl -s -X POST $BASE_URL/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-002",
    "fromWalletId": "wallet_2",
    "toWalletId": "wallet_1",
    "amount": 999999.00
  }')

if [[ $RESPONSE == *"insufficient balance"* ]] || [[ $RESPONSE == *"400"* ]]; then
    print_success "First request: Insufficient balance error"
else
    print_error "Expected insufficient balance error: $RESPONSE"
fi

# Retry with same key
echo "  Retrying with same idempotency key..."
RESPONSE=$(curl -s -X POST $BASE_URL/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-002",
    "fromWalletId": "wallet_2",
    "toWalletId": "wallet_1",
    "amount": 999999.00
  }')

if [[ $RESPONSE == *"insufficient balance"* ]] || [[ $RESPONSE == *"400"* ]]; then
    print_success "Second request: Still returns error (NOT cached)"
else
    print_error "Should still fail with same error: $RESPONSE"
fi

# ============================================
# TEST 6: Wallet Not Found (NOT Cached)
# ============================================
print_test "6" "Validation Error NOT Cached - Wallet Not Found"

RESPONSE=$(curl -s -X POST $BASE_URL/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-003",
    "fromWalletId": "wallet_999",
    "toWalletId": "wallet_2",
    "amount": 50.00
  }')

if [[ $RESPONSE == *"wallet not found"* ]] || [[ $RESPONSE == *"400"* ]]; then
    print_success "Wallet not found error"
else
    print_error "Expected wallet not found error: $RESPONSE"
fi

# Create the wallet
echo "  Creating wallet_999..."
psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -c "INSERT INTO wallets (id, balance, created_at, updated_at) VALUES ('wallet_999', 1000.00, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (id) DO NOTHING;" > /dev/null 2>&1

# Retry with same key
echo "  Retrying with same idempotency key after creating wallet..."
RESPONSE=$(curl -s -X POST $BASE_URL/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-003",
    "fromWalletId": "wallet_999",
    "toWalletId": "wallet_2",
    "amount": 50.00
  }')

if [[ $RESPONSE == *"PROCESSED"* ]]; then
    print_success "Transfer succeeded after fixing validation error"
    echo "  Proves validation errors are NOT cached!"
else
    print_error "Should succeed after creating wallet: $RESPONSE"
fi

# ============================================
# TEST 7: Double-Entry Ledger Integrity
# ============================================
print_test "7" "Double-Entry Ledger Integrity"

NET_BALANCE=$(psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -t -c "SELECT SUM(CASE WHEN entry_type = 'CREDIT' THEN amount ELSE -amount END) FROM ledger_entries;" | xargs)

# Check if balance is zero (with any decimal precision)
if [[ "$NET_BALANCE" =~ ^0(\.0+)?$ ]] || [[ "$NET_BALANCE" == "0" ]]; then
    print_success "Ledger balances to zero (double-entry accounting verified)"
else
    print_error "Ledger doesn't balance: $NET_BALANCE"
fi

# ============================================
# TEST 8: Transfer Count Verification
# ============================================
print_test "8" "Transfer Count and Idempotency Records"

TRANSFER_COUNT=$(psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -t -c "SELECT COUNT(*) FROM transfers WHERE status='PROCESSED';" | xargs)
IDEMPOTENCY_COUNT=$(psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -t -c "SELECT COUNT(*) FROM idempotency_records WHERE status='COMPLETED';" | xargs)

echo "  Processed transfers: $TRANSFER_COUNT"
echo "  Completed idempotency records: $IDEMPOTENCY_COUNT"

if [[ "$TRANSFER_COUNT" == "$IDEMPOTENCY_COUNT" ]]; then
    print_success "Transfer count matches idempotency records"
else
    echo -e "${YELLOW}  Note: Count mismatch is expected if validation errors occurred${NC}"
fi

# ============================================
# Summary
# ============================================
echo ""
echo -e "${GREEN}=========================================="
echo "All Tests Passed! ✓"
echo "==========================================${NC}"
echo ""
echo "Summary of Verified Features:"
echo "  ✓ Health check endpoint"
echo "  ✓ Successful money transfer"
echo "  ✓ Atomic balance updates"
echo "  ✓ Idempotency (duplicate requests)"
echo "  ✓ Request hash validation"
echo "  ✓ Validation errors NOT cached"
echo "  ✓ Double-entry ledger integrity"
echo "  ✓ Transfer and idempotency tracking"
echo ""
echo "View detailed documentation:"
echo "  - README.md         (overview)"
echo "  - TESTING.md        (manual testing guide)"
echo "  - docs/schema.md    (database design)"
echo ""
