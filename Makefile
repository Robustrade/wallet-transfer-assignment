# Wallet Transfer Service — developer task runner.
# Run `make help` to list available targets.

# Mirror the CI commands so "green locally" means "green in CI".
LINT_CMD ?= golangci-lint run ./...
FORMAT_CHECK_CMD ?= test -z "$(shell gofmt -l .)"
TEST_CMD ?= CGO_ENABLED=1 go test ./... -race -cover -count=1

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help.
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## Run the server (in-memory store unless DATABASE_URL is set).
	go run ./cmd/server

.PHONY: smoke
smoke: ## Run the end-to-end curl smoke test against a running server (needs jq).
	./examples/smoke.sh

.PHONY: build
build: ## Compile the server binary into ./bin.
	go build -o bin/server ./cmd/server

.PHONY: test
test: ## Run all tests with the race detector and coverage (matches CI).
	$(TEST_CMD)

.PHONY: cover
cover: ## Generate and open an HTML coverage report.
	CGO_ENABLED=1 go test ./... -race -coverprofile=coverage.out -count=1
	go tool cover -html=coverage.out

.PHONY: lint
lint: ## Run golangci-lint (matches CI).
	$(LINT_CMD)

.PHONY: fmt
fmt: ## Format the codebase.
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Verify formatting without writing (matches CI).
	$(FORMAT_CHECK_CMD)

.PHONY: vet
vet: ## Run go vet.
	go vet ./...

.PHONY: tidy
tidy: ## Tidy the module graph.
	go mod tidy

.PHONY: check
check: fmt-check vet lint test ## Run the full pre-PR verification suite.

.PHONY: docker-build
docker-build: ## Build the production container image.
	docker build -t wallet-transfer-service:latest .

.PHONY: compose-up
compose-up: ## Start the service and PostgreSQL via docker compose.
	docker compose up --build

.PHONY: compose-down
compose-down: ## Stop and remove the docker compose stack.
	docker compose down -v
