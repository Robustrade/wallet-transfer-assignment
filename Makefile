.DEFAULT_GOAL := help

# Local Postgres started by docker-compose.yml. Override for another server:
#   make test-pg PG_DSN='postgres://me@localhost:5432/wallet?sslmode=disable'
PG_DSN ?= postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable
# A DSN allowed to CREATE DATABASE, used by verify-pg.
PG_ADMIN_DSN ?= postgres://wallet:wallet@localhost:5432/postgres?sslmode=disable

GOLANGCI_LINT ?= $(shell command -v golangci-lint 2>/dev/null || echo $(shell go env GOPATH)/bin/golangci-lint)

.PHONY: help
help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-10s %s\n", $$1, $$2}'

.PHONY: build
build: ## Compile all packages
	go build ./...

.PHONY: run
run: ## Run the server on SQLite (file:wallet.db)
	go run ./cmd/server

.PHONY: run-pg
run-pg: ## Start Postgres in Docker and run the server against it
	docker compose up -d --wait postgres
	DB_DRIVER=postgres DB_DSN="$(PG_DSN)" go run ./cmd/server

.PHONY: test
test: ## Run all tests with the race detector (SQLite only)
	go test -race -count=1 ./...

.PHONY: test-pg
test-pg: ## Run all tests on SQLite and Postgres
	docker compose up -d --wait postgres
	TEST_POSTGRES_DSN="$(PG_DSN)" go test -race -count=1 ./...

.PHONY: verify-pg
verify-pg: ## End-to-end check on Postgres: tests, real server, demo, SQL invariants (docs/postgres.md)
	PG_ADMIN_DSN="$(PG_ADMIN_DSN)" ./scripts/verify-postgres.sh

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

.PHONY: fmt-check
fmt-check: ## Fail if any file needs gofmt
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "needs gofmt:"; echo "$$out"; exit 1; fi

.PHONY: demo
demo: ## Run the demo script against a running server
	./scripts/demo.sh
