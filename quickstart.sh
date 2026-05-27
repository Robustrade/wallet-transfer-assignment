#!/bin/bash

# Wallet Transfer Service - One-Click Quickstart
# Runs setup, starts server, and opens test instructions

set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${BLUE}╔══════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║  Wallet Transfer Service - Quick Start      ║${NC}"
echo -e "${BLUE}╚══════════════════════════════════════════════╝${NC}"
echo ""

# Step 1: Setup
echo -e "${YELLOW}[1/3]${NC} Running setup..."
./scripts/setup.sh

# Step 2: Start server in background
echo ""
echo -e "${YELLOW}[2/3]${NC} Starting server in background..."
./scripts/run.sh > server.log 2>&1 &
SERVER_PID=$!
echo -e "${GREEN}✓ Server started (PID: $SERVER_PID)${NC}"
echo "  View logs: tail -f server.log"

# Wait for server to be ready
echo ""
echo "Waiting for server to be ready..."
for i in {1..10}; do
    if curl -s http://localhost:8080/health > /dev/null 2>&1; then
        echo -e "${GREEN}✓ Server is ready!${NC}"
        break
    fi
    if [ $i -eq 10 ]; then
        echo -e "${RED}✗ Server failed to start. Check server.log${NC}"
        kill $SERVER_PID 2>/dev/null || true
        exit 1
    fi
    sleep 1
done

# Step 3: Run tests
echo ""
echo -e "${YELLOW}[3/3]${NC} Running automated tests..."
echo ""
./scripts/test.sh

# Keep server running
echo ""
echo -e "${GREEN}╔══════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║          All Tests Passed! ✓                 ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════════╝${NC}"
echo ""
echo "Server is running on http://localhost:8080"
echo "  Server PID: $SERVER_PID"
echo "  View logs: tail -f server.log"
echo ""
echo "To stop the server:"
echo -e "  ${BLUE}kill $SERVER_PID${NC}"
echo ""
echo "To run tests again:"
echo -e "  ${BLUE}./scripts/test.sh${NC}"
echo ""
echo "Documentation:"
echo "  - README.md        (overview)"
echo "  - TESTING.md       (manual testing)"
echo "  - docs/schema.md   (database design)"
echo ""
