DATABASE_URL ?= postgres://admin:<password>@localhost:5432/mydb?sslmode=disable
TEST_DATABASE_URL ?=

.PHONY: up down run build test test-integration lint fmt-check tidy

up: ## Start Postgres via docker-compose
	docker compose up -d postgres

down: ## Stop and remove docker-compose services
	docker compose down

run: ## Run the API server locally (requires DATABASE_URL / Postgres running)
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/server

build: ## Compile the server binary
	go build -o bin/server ./cmd/server

test: ## Run service and HTTP tests with repository mocks
	go test ./... -cover

test-integration: ## Run Postgres integration tests (starts Testcontainers by default)
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -tags=integration ./internal/repository/postgres/... -v

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt-check: ## Fail if any file is not gofmt-formatted
	test -z "$$(gofmt -l .)"

tidy: ## Tidy go.mod/go.sum
	go mod tidy
