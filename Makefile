.PHONY: help build test clean run docker-up docker-down db-migrate lint fmt

help:
	@echo "Available commands:"
	@echo "  make build        - Build the application"
	@echo "  make test         - Run all tests"
	@echo "  make test-unit    - Run unit tests"
	@echo "  make test-int     - Run integration tests"
	@echo "  make test-cov     - Run tests with coverage"
	@echo "  make clean        - Remove build artifacts"
	@echo "  make run          - Run the application locally"
	@echo "  make docker-up    - Start Docker containers"
	@echo "  make docker-down  - Stop Docker containers"
	@echo "  make lint         - Run linter"
	@echo "  make fmt          - Format code"

build:
	@echo "Building application..."
	@go build -o bin/server ./cmd/server

test:
	@echo "Running all tests..."
	@go test -v ./...

test-unit:
	@echo "Running unit tests..."
	@go test -v -short ./tests/unit/...

test-int:
	@echo "Running integration tests..."
	@go test -v ./tests/integration/...

test-cov:
	@echo "Running tests with coverage..."
	@go test -v -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

clean:
	@echo "Cleaning build artifacts..."
	@rm -f bin/server
	@rm -f coverage.out coverage.html

run: build
	@echo "Running application..."
	@./bin/server

docker-up:
	@echo "Starting Docker containers..."
	@docker-compose -f docker/docker-compose.yml up -d

docker-down:
	@echo "Stopping Docker containers..."
	@docker-compose -f docker/docker-compose.yml down

docker-logs:
	@docker-compose -f docker/docker-compose.yml logs -f app

lint:
	@echo "Running linter..."
	@golangci-lint run ./...

fmt:
	@echo "Formatting code..."
	@go fmt ./...
