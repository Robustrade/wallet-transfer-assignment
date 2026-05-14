#!/bin/bash
set -e

echo "Running Unit Tests..."
go test ./internal/...

echo "Starting DB for Integration Tests..."
docker compose up db -d

# Wait for DB to be ready
echo "Waiting for database..."
until docker exec $(docker compose ps -q db) pg_isready -U admin > /dev/null 2>&1; do
  sleep 1
done

echo "Running Integration Tests..."
DB_HOST=localhost DB_PORT=5432 DB_USER=admin DB_PASSWORD=admin123 DB_NAME=my_db go test ./tests/... -v