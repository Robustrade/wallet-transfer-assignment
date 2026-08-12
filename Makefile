SHELL := /bin/bash
.DEFAULT_GOAL := help

# The default docker context on this machine may point at a remote host that
# is not always reachable, so docker calls are pinned to the local daemon.
# Override with: make db-up DOCKER_CONTEXT=default
DOCKER_CONTEXT ?= desktop-linux
DOCKER  := docker --context $(DOCKER_CONTEXT)
COMPOSE := $(DOCKER) compose

MODULE       := github.com/devthedeveloper/wallet-transfer-assignment
POSTGRES_PORT ?= 5440
DATABASE_URL  ?= postgres://wallet:wallet@localhost:$(POSTGRES_PORT)/walletdb?sslmode=disable

-include .env
export

.PHONY: help
help: ## show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}'

# --------------------------------------------------------------------- build
.PHONY: build run
build: ## compile the walletd binary
	go build -o bin/walletd ./cmd/walletd

run: ## run walletd against DATABASE_URL (applies migrations on startup)
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/walletd

# ---------------------------------------------------------------------- go
.PHONY: fmt vet lint tidy
fmt: ## check gofmt is clean
	@test -z "$$(gofmt -l .)" || { echo "gofmt needs to be run on:"; gofmt -l .; exit 1; }

vet: ## go vet
	go vet ./...

lint: ## run golangci-lint
	golangci-lint run ./...

tidy: ## go mod tidy
	go mod tidy

# -------------------------------------------------------------------- test
.PHONY: test-unit test test-integration cover
test-unit: ## run unit tests only, no database required
	go test ./... -race -count=1 -short

test: db-up ## start postgres, wait healthy, run the full suite (unit + integration)
	@echo "waiting for postgres to be healthy..."
	@until $(DOCKER) inspect --format='{{json .State.Health.Status}}' wallet-transfer-postgres 2>/dev/null | grep -q healthy; do sleep 1; done
	TEST_DATABASE_URL="$(DATABASE_URL)" go test ./... -race -count=1

test-integration: db-up ## run only the integration suite
	@until $(DOCKER) inspect --format='{{json .State.Health.Status}}' wallet-transfer-postgres 2>/dev/null | grep -q healthy; do sleep 1; done
	TEST_DATABASE_URL="$(DATABASE_URL)" go test ./test/... ./internal/postgres/... -race -count=1

cover: db-up ## run tests with coverage report
	@until $(DOCKER) inspect --format='{{json .State.Health.Status}}' wallet-transfer-postgres 2>/dev/null | grep -q healthy; do sleep 1; done
	TEST_DATABASE_URL="$(DATABASE_URL)" go test ./... -race -count=1 -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

# ------------------------------------------------------------------- docker
.PHONY: db-up db-down db-psql migrate
db-up: ## start postgres via docker compose
	$(COMPOSE) up -d postgres

db-down: ## stop and remove postgres, dropping its data volume
	$(COMPOSE) down -v

db-psql: ## open a psql shell against the local postgres
	$(DOCKER) exec -it wallet-transfer-postgres psql -U wallet -d walletdb

migrate: ## apply migrations against DATABASE_URL
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/walletd -migrate-only
