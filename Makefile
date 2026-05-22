.PHONY: run seed test-unit test-integration test db-up db-down tidy lint

# Start the main server
run:
	go run cmd/server/main.go

# Seed the database with initial test wallets
seed:
	go run cmd/seed/main.go

# Run linters
lint:
	golangci-lint run ./...

# Run isolated unit tests (fast, no DB required)
test-unit:
	go test -v ./internal/tests/unit/...

# Run integration tests (requires Docker for Testcontainers)
test-integration:
	go test -v ./internal/tests/integration/...

# Run all tests in the project
test: test-unit test-integration

# Start the local PostgreSQL database via Docker Compose
db-up:
	docker-compose up -d

# Stop and remove the local PostgreSQL database
db-down:
	docker-compose down

# Clean up and verify Go modules
tidy:
	go mod tidy
	go mod verify
