DATABASE_URL ?= postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable
BASE_URL     ?= http://localhost:8080
export DATABASE_URL

.PHONY: help
help: ## Show available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: db
db: ## Start postgres in Docker
	docker compose up -d postgres
	@echo "waiting for postgres to become ready..."
	@for i in $$(seq 1 60); do \
		docker compose exec -T postgres pg_isready -U wallet -d wallet >/dev/null 2>&1 && echo "postgres ready" && exit 0; \
		sleep 1; \
	done; \
	echo "postgres failed to become ready"; exit 1

.PHONY: down
down: ## Stop postgres container and remove volumes
	docker compose down -v

.PHONY: seed
seed: ## Apply migrations and seed demo wallets
	go run ./cmd/seed

.PHONY: migrate
migrate: ## Apply migrations only
	go run ./cmd/seed -migrate-only

.PHONY: run
run: ## Run the API server locally
	APP_ENV=dev go run ./cmd/server

.PHONY: test
test: ## Run unit and integration tests
	go test -v ./...

.PHONY: test-race
test-race: ## Run tests under the race detector
	go test -race -v ./...

.PHONY: smoke
smoke: ## Run end-to-end smoke tests against running API
	BASE_URL=$(BASE_URL) ./scripts/smoke.sh

.PHONY: fmt
fmt: ## Format Go source files
	gofmt -s -w .

.PHONY: fmt-check
fmt-check: ## Check code formatting
	@test -z "$$(gofmt -s -l . | tee /dev/stderr)" || { echo "Code not formatted. Run: make fmt"; exit 1; }
