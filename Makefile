.PHONY: help dev-up dev-down migrate-up migrate-down migrate-status sqlc fmt

help: ## Display available targets
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

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

fmt: ## Format all Go code
	go fmt ./...
