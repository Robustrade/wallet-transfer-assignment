SHELL := /bin/bash

# Defaults can be overridden via the environment, e.g.:
#   DATABASE_URL=... make migrate-up
DATABASE_URL ?= postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable
# For assignment scope, integration tests default to the same Postgres
# instance used by `make run` / `make seed` (a single dev DB). The tests
# TRUNCATE all tables in setupTestEnv, so running `make test-int` will wipe
# any rows created interactively. Override INTEGRATION_DATABASE_URL to point
# at a separate database if you want to keep the dev data intact.
INTEGRATION_DATABASE_URL ?= $(DATABASE_URL)
PORT ?= 8080

.PHONY: all
all: build ## Default target — build the server binary.

.PHONY: clean
clean: ## Remove build artifacts and coverage output.
	rm -rf bin/ coverage/

.PHONY: build
build: ## Build the server binary.
	go build -trimpath -o bin/server ./cmd/server

.PHONY: run
run: ## Run the server locally (requires DATABASE_URL).
	DATABASE_URL=$(DATABASE_URL) PORT=$(PORT) go run ./cmd/server

.PHONY: test
test: ## Run unit tests with the race detector and coverage.
	go test -race -count=1 -cover ./...

.PHONY: coverage
coverage: ## Write a coverage profile to coverage/cover.out and an HTML report.
	@mkdir -p coverage
	go test -race -count=1 -coverprofile=coverage/cover.out ./...
	go tool cover -html=coverage/cover.out -o coverage/cover.html
	@echo "wrote coverage/cover.html"

.PHONY: test-int
test-int: ## Run integration tests against a real Postgres (requires INTEGRATION_DATABASE_URL).
	INTEGRATION_DATABASE_URL=$(INTEGRATION_DATABASE_URL) \
		go test -race -count=1 -tags=integration ./tests/integration/...

.PHONY: test-all
test-all: test test-int ## Run unit + integration tests.

.PHONY: lint
lint: ## Run golangci-lint.
	golangci-lint run ./...

.PHONY: fmt
fmt: ## Format the code.
	gofmt -s -w .

.PHONY: fmt-check
fmt-check: ## Fail if any file is not gofmt-formatted.
	@diff=$$(gofmt -l . | grep -v '^vendor/' || true); \
		if [ -n "$$diff" ]; then \
			echo "files not gofmt-clean:"; echo "$$diff"; exit 1; \
		fi

.PHONY: migrate-up
migrate-up: ## Apply all pending migrations.
	migrate -path migrations -database "$(DATABASE_URL)" up

.PHONY: migrate-down
migrate-down: ## Roll back the last migration.
	migrate -path migrations -database "$(DATABASE_URL)" down 1

.PHONY: seed
seed: ## Insert two seed wallets used by manual verification.
	psql "$(DATABASE_URL)" -c "INSERT INTO wallets (id, balance_minor, currency) VALUES \
('wallet_1', 10000, 'INR'), ('wallet_2', 0, 'INR') \
ON CONFLICT (id) DO UPDATE SET balance_minor = EXCLUDED.balance_minor, updated_at = NOW();"

.PHONY: compose-up
compose-up: ## Start postgres + app via docker compose.
	docker compose up --build -d

.PHONY: compose-down
compose-down: ## Stop docker compose stack.
	docker compose down -v

.PHONY: pg-up
pg-up: ## Start only postgres (for local development).
	docker compose up -d postgres

.PHONY: pg-down
pg-down: ## Stop postgres.
	docker compose stop postgres

.PHONY: help
help: ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
