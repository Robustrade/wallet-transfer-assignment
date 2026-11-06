GO               ?= go
PKGS             := ./...
BIN_DIR          := bin
COMPOSE          ?= docker compose
GO_IMAGE         := golang:1.24-bookworm
GOLANGCI_IMAGE   := golangci/golangci-lint:v1.64.8

.PHONY: help up down reset logs ps demo test lint fmt-check build fmt fmt-check-local lint-local test-local

help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

# ---- Docker targets (primary interface; only Docker required) ----------------

up: ## Build and start db -> migrate+seed -> app; wait until the API is healthy
	$(COMPOSE) up --build --detach --wait app
	@echo "API ready on http://localhost:$${APP_HOST_PORT:-8080}"

down: ## Stop the stack (data is kept)
	$(COMPOSE) down

reset: ## Stop the stack and delete its data volume
	$(COMPOSE) down --volumes --remove-orphans

logs: ## Follow application logs
	$(COMPOSE) logs --follow app

ps: ## Show service status
	$(COMPOSE) ps --all

demo: ## Walk through the API guarantees against the running stack (run `make up` first)
	./scripts/demo.sh

test: ## Run all tests (unit + integration + concurrency) in Docker against a throwaway Postgres
	$(COMPOSE) --profile test build test
	$(COMPOSE) --profile test run --rm test; status=$$?; \
		$(COMPOSE) --profile test rm --stop --force --volumes db-test >/dev/null 2>&1; \
		exit $$status

lint: ## Run golangci-lint (same version as CI) in Docker
	docker run --rm -v "$(CURDIR)":/app -w /app $(GOLANGCI_IMAGE) golangci-lint run $(PKGS)

fmt-check: ## Fail if any Go file is not gofmt-formatted (in Docker)
	docker run --rm -v "$(CURDIR)":/src -w /src $(GO_IMAGE) \
		sh -c 'out=$$(gofmt -l .); test -z "$$out" || { echo "Files need gofmt:"; echo "$$out"; exit 1; }'

# ---- Local targets (need Go and, for integration tests, Postgres) -----------

build: ## Build the wallet binary into ./bin
	CGO_ENABLED=0 $(GO) build -trimpath -o $(BIN_DIR)/wallet ./cmd/wallet

fmt: ## Format all Go files
	gofmt -w .

fmt-check-local: ## Fail if any Go file is not gofmt-formatted
	@test -z "$$(gofmt -l .)" || { echo "Files need gofmt:"; gofmt -l .; exit 1; }

lint-local: ## Run golangci-lint from PATH
	golangci-lint run $(PKGS)

test-local: ## Run tests locally; set TEST_DATABASE_URL to include integration tests
	$(GO) test $(PKGS) -race -cover -count=1
