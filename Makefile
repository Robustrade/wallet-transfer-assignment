.PHONY: run test test-unit test-integration test-ci lint fmt-check docker-db docker-db-stop clean up help

# Default target showing help instruction
help:
	@echo "Available commands:"
	@echo "  make up               - Spin up PostgreSQL and start the API server"
	@echo "  make run              - Run the API server locally"
	@echo "  make test             - Run all tests (unit and integration)"
	@echo "  make test-unit        - Run only unit tests (excluding testcontainers-go integration tests)"
	@echo "  make test-integration - Run database integration and concurrency tests (requires Docker)"
	@echo "  make test-ci          - Run CI test suite (with race detector and coverage)"
	@echo "  make lint             - Run golangci-lint on the codebase"
	@echo "  make fmt-check        - Check formatting of Go source files"
	@echo "  make docker-db        - Spin up a local development PostgreSQL container on port 5435"
	@echo "  make docker-db-stop   - Stop and remove the local PostgreSQL container"
	@echo "  make clean            - Clean Go test caches and build targets"

up: docker-db
	@echo "Waiting for database to be ready..."
	@sleep 2 2>/dev/null || timeout /t 2 >nul || ping -n 3 127.0.0.1 >nul
	@echo "Starting application server..."
	go run server/main.go

run:
	go run server/main.go

test:
	go test -v ./...

test-unit:
	go test -v ./tests/wallet/... ./tests/transfer/... ./tests/ledger/...

test-integration:
	go test -v ./tests/integration/...

test-ci:
	go test ./... -race -cover

lint:
	golangci-lint run ./...

fmt-check:
	@# Checks if files are formatted. Returns exit code 1 if any unformatted file is found.
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "Unformatted files found:"; \
		gofmt -l .; \
		exit 1; \
		else \
		echo "All files formatted correctly."; \
	fi

docker-db:
	docker start wallet-db 2>/dev/null || docker run --name wallet-db -e POSTGRES_DB=Wallet -e POSTGRES_PASSWORD=Abc123 -p 5435:5432 -d postgres:15-alpine

docker-db-stop:
	docker stop wallet-db || true
	docker rm wallet-db || true

clean:
	go clean -testcache

