# Makefile for the wallet-transfer service.
#
# Pin the Go toolchain to match go.mod so local builds and CI agree.
export GOTOOLCHAIN := go1.24.13

# Race detector requires cgo; mirror the CI environment.
CGO_ENABLED ?= 1

COMPOSE := docker compose -f deployments/docker-compose.yml

.DEFAULT_GOAL := help

.PHONY: help build run test test-integration lint fmt fmt-check vet \
        compose-up compose-down tidy

help: ## Show this help.
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

build: ## Compile the walletd binary into ./bin.
	CGO_ENABLED=0 go build -trimpath -o bin/walletd ./cmd/walletd

run: ## Run the service locally (requires DATABASE_URL).
	@test -n "$(DATABASE_URL)" || { echo "DATABASE_URL is required, e.g. postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable"; exit 1; }
	go run ./cmd/walletd

test: ## Run all tests with the race detector and coverage. Integration tests self-skip without Docker.
	CGO_ENABLED=$(CGO_ENABLED) go test ./... -race -cover

test-integration: ## Run the Postgres-backed integration tests (requires Docker / testcontainers).
	CGO_ENABLED=$(CGO_ENABLED) go test ./internal/repository/... ./test/integration/... -race -count=1

lint: ## Run golangci-lint over the whole module.
	golangci-lint run ./...

fmt: ## Format all Go source in place.
	gofmt -w .

fmt-check: ## Fail if any Go source is not gofmt-clean (CI format gate).
	@test -z "$$(gofmt -l .)" || { echo "gofmt found unformatted files:"; gofmt -l .; exit 1; }

vet: ## Run go vet over the module.
	go vet ./...

tidy: ## Tidy and verify module dependencies.
	go mod tidy

compose-up: ## Build and start the full stack (db + app) via docker compose.
	$(COMPOSE) up --build

compose-down: ## Stop the stack and remove its volumes.
	$(COMPOSE) down -v
