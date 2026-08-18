.PHONY: help db-up db-down db-reset db-psql run build test test-unit test-integration lint fmt fmt-check vet tidy

# Recipes below use POSIX shell syntax (VAR=val cmd, $$(), if [ ]; then).
# On Windows, GNU Make defaults to cmd.exe for recipes unless told otherwise,
# and cmd.exe can't parse any of that — so force Git Bash explicitly rather
# than relying on `bash` resolving via PATH (Windows also ships a broken WSL
# launcher at C:\Windows\system32\bash.exe that can shadow the real one).
ifeq ($(OS),Windows_NT)
SHELL := C:/Program Files/Git/bin/bash.exe
.SHELLFLAGS := -c
endif

DATABASE_URL ?= postgres://postgres:root@localhost:5432/wallet_schema_db

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*## "}; {printf "  %-14s %s\n", $$1, $$2}'

db-up: ## Start Postgres (docker compose), applying the schema on first run
	docker compose up -d

db-down: ## Stop Postgres, keeping data
	docker compose down

db-reset: ## Stop Postgres and wipe its data, then start fresh (re-applies schema)
	docker compose down -v
	docker compose up -d

db-psql: ## Open a psql shell against the running container
	docker exec -it wallet-transfer-postgres psql -U postgres -d wallet_schema_db

run: ## Run the API server (listens on :8080, needs db-up first)
	DATABASE_URL=$(DATABASE_URL) go run ./cmd/api

build: ## Build the API binary
	go build -o bin/api ./cmd/api

test: ## Run the full suite (unit + integration; needs db-up first)
	DATABASE_URL=$(DATABASE_URL) go test ./...

test-unit: ## Run only the DB-free unit tests (models, service, handler)
	go test ./internals/models/... ./internals/service/... ./internals/handler/...

test-integration: ## Run only the Postgres-backed integration tests (needs db-up first)
	DATABASE_URL=$(DATABASE_URL) go test ./internals/tests/... -v

vet: ## go vet
	go vet ./...

fmt: ## Format all Go files
	gofmt -w .

fmt-check: ## Fail if any Go file isn't gofmt-formatted
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then \
		echo "not gofmt-formatted:"; echo "$$files"; exit 1; \
	fi

tidy: ## Tidy go.mod/go.sum
	go mod tidy
