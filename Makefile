.PHONY: help setup dev-up dev-down migrate-up migrate-down migrate-status sqlc mock build run test fmt

export PATH := $(shell go env GOPATH)/bin:$(PATH)

help: ## Display available targets
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

setup: ## Install missing development tools (goose, sqlc, mockgen)
	@echo "Checking development tools..."
	@which goose > /dev/null 2>&1 || (echo "Installing goose..." && go install github.com/pressly/goose/v3/cmd/goose@v3.24.1)
	@which sqlc > /dev/null 2>&1 || (echo "Installing sqlc..." && go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0)
	@which mockgen > /dev/null 2>&1 || (echo "Installing mockgen..." && go install go.uber.org/mock/mockgen@v0.5.0)
	@echo "All required development tools are available."

dev-up: ## Start local PostgreSQL 18 container
	docker compose -f docker-compose.dev.yml up -d

dev-down: ## Stop and remove local PostgreSQL container and volumes
	docker compose -f docker-compose.dev.yml down -v

# Database configuration
POSTGRES_USER     ?= postgres
POSTGRES_PASSWORD ?= postgres
POSTGRES_HOST     ?= localhost
POSTGRES_PORT     ?= 5433
POSTGRES_DB       ?= wallet_db
MIGRATIONS_DIR    ?= internal/repository/migrations

DB_STRING ?= "postgresql://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@$(POSTGRES_HOST):$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable"

migrate-up: ## Apply pending database migrations
	goose -dir $(MIGRATIONS_DIR) postgres $(DB_STRING) up

migrate-down: ## Rollback last database migration
	goose -dir $(MIGRATIONS_DIR) postgres $(DB_STRING) down

migrate-status: ## Check database migration status
	goose -dir $(MIGRATIONS_DIR) postgres $(DB_STRING) status

sqlc: ## Generate type-safe Go code from SQL queries
	sqlc generate

mock: ## Generate mocks using mockgen
	mockgen -source=internal/repository/wallet_repo.go -destination=internal/mocks/mock_wallet_repo.go -package=mocks
	mockgen -source=internal/repository/transfer_repo.go -destination=internal/mocks/mock_transfer_repo.go -package=mocks
	mockgen -source=internal/repository/ledger_repo.go -destination=internal/mocks/mock_ledger_repo.go -package=mocks
	mockgen -source=internal/repository/idempotency_repo.go -destination=internal/mocks/mock_idempotency_repo.go -package=mocks
	mockgen -source=internal/repository/transactor.go -destination=internal/mocks/mock_transactor.go -package=mocks

build: ## Build the API server binary
	go build -o bin/api cmd/api/main.go

run: ## Run the API server
	go run cmd/api/main.go

test: ## Run unit and integration tests
	go test -v ./...

fmt: ## Format all Go code
	go fmt ./...
