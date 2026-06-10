# ==============================================================================
# Wallet Transfer Service Management
# ==============================================================================
# This Makefile provides a set of commands to manage the development lifecycle 
# of the wallet-server, including building, testing, linting, and containerization.
#
# Prerequisites:
# - Go 1.26+
# - golangci-lint
# - Docker (for container builds)
# ==============================================================================

.PHONY: all build test lint format-check clean docker-build help

APP_NAME = wallet-server
CMD_PATH = ./cmd/api

all: lint-format-test build

build: ## Build the server binary
	go build -v -o bin/$(APP_NAME) $(CMD_PATH)

test: ## Run tests with race detection and coverage
	go test ./... -race -cover -v

lint: ## Run golangci-lint
	golangci-lint run ./...

format-check: ## Check for formatting issues
	test -z "$$(gofmt -l .)"

lint-format-test: lint format-check test ## Run all checks

clean: ## Remove build artifacts
	rm -rf bin/

docker-build: ## Build the Docker image
	docker build -t $(APP_NAME) .

help: ## Display this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

migrate-up: ## Run migrations (Requires DATABASE_URL to be set)
	@echo "Running migrations..."
	# Example using migrate CLI: migrate -path migrations/ -database "$$DATABASE_URL" up