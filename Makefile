.PHONY: help up down logs migrate-up migrate-down seed build run test test-unit fmt vet lint tidy

# Local database connection used by run/migrate/seed and the integration tests.
DATABASE_URL ?= postgres://myuser:mypassword@localhost:5432/mydb?sslmode=disable
MIGRATE ?= migrate -source file://./migrations -database "$(DATABASE_URL)"

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

up: ## Start Postgres (and Redis if present) via docker compose
	docker compose up -d postgres

down: ## Stop docker compose services
	docker compose down

logs: ## Tail app logs (when running under compose)
	docker compose logs -f app

migrate-up: ## Apply all schema migrations
	$(MIGRATE) up

migrate-down: ## Roll back the last migration
	$(MIGRATE) down 1

seed: ## Load dev/test wallets
	docker exec -i postgres_db psql -U myuser -d mydb < migrations/seed.sql

build: ## Build the binary
	go build -o bin/wallet .

run: build ## Run the service locally
	DATABASE_URL="$(DATABASE_URL)" ./bin/wallet

test: ## Run all tests (unit + integration). -p 1 serialises DB-sharing packages.
	TEST_DATABASE_URL="$(DATABASE_URL)" go test -p 1 -count=1 ./...

test-unit: ## Run only tests that need no database (integration tests skip anyway)
	go test $(shell go list ./... | grep -v /tests/integration)

cover: ## Run tests with coverage over business-logic packages
	TEST_DATABASE_URL="$(DATABASE_URL)" go test -p 1 -count=1 \
		-coverpkg=$$(go list ./internal/... | grep -vE '/tests/' | paste -sd, -) \
		-coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

fmt: ## Format code
	gofmt -w .

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint (must be installed)
	golangci-lint run ./...

tidy: ## Tidy modules
	go mod tidy
