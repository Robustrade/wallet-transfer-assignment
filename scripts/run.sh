#!/bin/bash

# Wallet Transfer Service - Run Script
# Starts the server with test data

set -e  # Exit on error

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "=========================================="
echo "Wallet Transfer Service - Starting Server"
echo "=========================================="
echo ""

# Check if binary exists
if [ ! -f "bin/server" ]; then
    echo -e "${YELLOW}Binary not found. Building...${NC}"
    go build -o bin/server ./cmd/server
    echo -e "${GREEN}✓ Build complete${NC}"
    echo ""
fi

# Extract database credentials from config.json
DB_NAME=$(grep -o '"dbname": *"[^"]*"' config.json | cut -d'"' -f4)
DB_USER=$(grep -o '"user": *"[^"]*"' config.json | cut -d'"' -f4)
DB_HOST=$(grep -o '"host": *"[^"]*"' config.json | cut -d'"' -f4)
DB_PORT=$(grep -A 6 '"database"' config.json | grep -o '"port": *"[^"]*"' | head -1 | cut -d'"' -f4)

# Seed test data
echo "Loading test data..."
psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -f scripts/seed_data.sql > /dev/null 2>&1
echo -e "${GREEN}✓ Test wallets created (wallet_1: $1000, wallet_2: $500)${NC}"
echo ""

echo -e "${GREEN}Starting server on http://localhost:8080${NC}"
echo ""
echo "Press Ctrl+C to stop the server"
echo "Run './scripts/test.sh' in another terminal to test the API"
echo ""
echo "==========================================\n"

# Start the server
./bin/server
