#!/bin/bash

# Wallet Transfer Service - Automated Setup Script
# This script sets up the database and runs all migrations

set -e  # Exit on error

echo "=========================================="
echo "Wallet Transfer Service - Setup"
echo "=========================================="
echo ""

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if config.json exists
if [ ! -f "config.json" ]; then
    echo -e "${YELLOW}config.json not found. Copying from config.example.json...${NC}"
    cp config.example.json config.json
    echo -e "${GREEN}✓ Created config.json${NC}"
    echo -e "${YELLOW}Note: Update config.json with your database credentials if needed${NC}"
    echo ""
fi

# Extract database credentials from config.json
DB_NAME=$(grep -o '"dbname": *"[^"]*"' config.json | cut -d'"' -f4)
DB_USER=$(grep -o '"user": *"[^"]*"' config.json | cut -d'"' -f4)
DB_HOST=$(grep -o '"host": *"[^"]*"' config.json | cut -d'"' -f4)
DB_PORT=$(grep -A 6 '"database"' config.json | grep -o '"port": *"[^"]*"' | head -1 | cut -d'"' -f4)

echo "Database Configuration:"
echo "  Host: $DB_HOST"
echo "  Port: $DB_PORT"
echo "  User: $DB_USER"
echo "  Database: $DB_NAME"
echo ""

# Check if PostgreSQL is running
echo "Checking PostgreSQL connection..."
if ! psql -h $DB_HOST -p $DB_PORT -U $DB_USER -lqt > /dev/null 2>&1; then
    echo -e "${RED}✗ Cannot connect to PostgreSQL${NC}"
    echo "  Make sure PostgreSQL is running:"
    echo "    macOS: brew services start postgresql"
    echo "    Linux: sudo systemctl start postgresql"
    exit 1
fi
echo -e "${GREEN}✓ PostgreSQL is running${NC}"
echo ""

# Drop and recreate database for fresh start
echo "Setting up database..."
psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d postgres > /dev/null 2>&1 <<EOF
DROP DATABASE IF EXISTS $DB_NAME;
CREATE DATABASE $DB_NAME;
EOF
echo -e "${GREEN}✓ Database created (fresh)${NC}"
echo ""

# Run migrations
echo "Running database migrations..."
psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -f migrations/001_create_schema.up.sql > /dev/null 2>&1
echo -e "${GREEN}✓ Migrations completed${NC}"
echo ""

# Install Go dependencies
echo "Installing Go dependencies..."
go mod download > /dev/null 2>&1
echo -e "${GREEN}✓ Dependencies installed${NC}"
echo ""

# Build the application
echo "Building application..."
go build -o bin/server ./cmd/server > /dev/null 2>&1
echo -e "${GREEN}✓ Application built successfully${NC}"
echo ""

echo -e "${GREEN}=========================================="
echo "Setup completed successfully!"
echo "==========================================${NC}"
echo ""
echo "Next steps:"
echo "  1. Run: ./scripts/run.sh        # Start the server"
echo "  2. Run: ./scripts/test.sh       # Run automated tests"
echo ""
