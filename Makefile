.PHONY: all build run test test-unit test-integration test-race lint clean migrate-up migrate-down docker-up docker-down

# Go parameters
BINARY_NAME=wallet-transfer
GO=go
GOTEST=$(GO) test
GOFLAGS=-v

# Database
DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/wallet_transfer?sslmode=disable

all: lint test build

## build: Build the application binary
build:
	$(GO) build -o bin/$(BINARY_NAME) ./cmd/server

## run: Run the application
run:
	DATABASE_URL=$(DATABASE_URL) $(GO) run ./cmd/server

## test: Run all tests
test:
	$(GOTEST) $(GOFLAGS) ./...

## test-unit: Run unit tests only
test-unit:
	$(GOTEST) $(GOFLAGS) ./tests/unit/...

## test-integration: Run integration tests only
test-integration:
	$(GOTEST) $(GOFLAGS) -count=1 ./tests/integration/...

## test-race: Run all tests with race detector
test-race:
	$(GOTEST) $(GOFLAGS) -race -count=1 ./...

## lint: Run linter
lint:
	golangci-lint run ./...

## clean: Clean build artifacts
clean:
	rm -rf bin/
	$(GO) clean

## migrate-up: Run database migrations
migrate-up:
	migrate -path migrations -database "$(DATABASE_URL)" up

## migrate-down: Run database migrations down
migrate-down:
	migrate -path migrations -database "$(DATABASE_URL)" down

## docker-up: Start Docker services
docker-up:
	docker-compose up -d

## docker-down: Stop Docker services
docker-down:
	docker-compose down

## help: Show this help
help:
	@echo "Available targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':'
