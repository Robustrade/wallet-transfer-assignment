.PHONY: help run test test-short lint fmt fmt-check generate tools db-up db-down tidy

export DATABASE_URL ?= postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

tools: ## Install dev tools (stringer, golangci-lint)
	@echo "Installing dev tools..."
	go install golang.org/x/tools/cmd/stringer@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
	@echo "Done."

generate: ## Run code generation (stringer)
	@echo "Running go generate..."
	go generate ./...
	@echo "Done."

run: ## Run the HTTP server (needs Postgres — start with make db-up first)
	@echo "Starting the server on :8080 ..."
	go run ./cmd/server

test: ## Full test suite — concurrency + integration (needs Docker running)
	@echo "Running full test suite with race detector (Docker needed)..."
	go test ./... -race -count=1
	@echo "All tests passed!"

test-short: ## Fast unit tests only, no Docker — what CI runs
	@echo "Running fast tests (no Docker)..."
	go test ./... -short -race -count=1
	@echo "All fast tests passed!"

lint: ## Run golangci-lint
	@echo "Running linter..."
	golangci-lint run ./...

fmt: ## Format all Go files
	@echo "Formatting code..."
	gofmt -w .

fmt-check: ## Check formatting — fails if anything needs gofmt
	@test -z "$$(gofmt -l .)" || { echo "These files need formatting:"; gofmt -l .; exit 1; }

tidy: ## Clean up go.mod and go.sum
	@echo "Tidying go.mod..."
	go mod tidy

db-up: ## Start local Postgres in Docker
	@echo "Starting Postgres..."
	docker compose up -d db
	@echo "Postgres is up on port 5432."

db-down: ## Stop and remove local Postgres
	@echo "Stopping Postgres..."
	docker compose down -v
	@echo "Postgres stopped."
