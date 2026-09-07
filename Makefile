.PHONY: help dev-up dev-down sqlc fmt

help: ## Display available targets
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

dev-up: ## Start local PostgreSQL 18 container
	docker compose -f docker-compose.dev.yml up -d

dev-down: ## Stop and remove local PostgreSQL container and volumes
	docker compose -f docker-compose.dev.yml down -v

sqlc: ## Generate type-safe Go code from SQL queries
	sqlc generate

fmt: ## Format all Go code
	go fmt ./...
