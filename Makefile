.PHONY: help build run test clean docker-up docker-down docker-logs migrate install-migrate coverage-core

# Colors for output
BLUE := \033[0;34m
GREEN := \033[0;32m
NC := \033[0m # No Color

help: ## Display this help screen
	@grep -h -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "$(BLUE)%-30s$(NC) %s\n", $$1, $$2}'

## Build & Run
build: ## Build the application binary
	@echo "$(GREEN)Building wallet-service...$(NC)"
	@go build -o bin/wallet-service ./cmd/server/main.go

run: ## Run the application
	@echo "$(GREEN)Running wallet-service...$(NC)"
	@go run ./cmd/server/main.go

run-watch: ## Run with hot reload (requires entr or CompileDaemon)
	@echo "$(GREEN)Running with hot reload...$(NC)"
	@which reflex > /dev/null || go install github.com/cespare/reflex@latest
	@reflex -r '\.go$$' -s -- go run ./cmd/server/main.go

## Docker
docker-build: ## Build Docker image
	@echo "$(GREEN)Building Docker image...$(NC)"
	@docker build -t wallet-transfer-service:latest .

docker-up: docker-build ## Start services with docker-compose
	@echo "$(GREEN)Starting services with docker-compose...$(NC)"
	@docker-compose up -d
	@echo "$(GREEN)Services started!$(NC)"
	@echo "Wallet Service: http://localhost:8080"
	@echo "Database: localhost:5432"

docker-down: ## Stop docker-compose services
	@echo "$(GREEN)Stopping services...$(NC)"
	@docker-compose down

docker-logs: ## View docker-compose logs
	@docker-compose logs -f

docker-clean: ## Remove docker images and volumes
	@echo "$(GREEN)Cleaning up Docker resources...$(NC)"
	@docker-compose down -v
	@docker rmi wallet-transfer-service:latest

## Database
install-migrate: ## Install golang-migrate CLI
	@echo "$(GREEN)Installing golang-migrate...$(NC)"
	@go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	@echo "$(GREEN)golang-migrate installed!$(NC)"

migrate-up: install-migrate ## Run all migrations up
	@echo "$(GREEN)Running migrations...$(NC)"
	@migrate -path migrations -database "$(DATABASE_URL)" -verbose up

migrate-down: install-migrate ## Rollback last migration
	@echo "$(GREEN)Rolling back migrations...$(NC)"
	@migrate -path migrations -database "$(DATABASE_URL)" -verbose down -steps 1

migrate-version: install-migrate ## Show current migration version
	@migrate -path migrations -database "$(DATABASE_URL)" version

## Testing
test: ## Run all tests with coverage
	@echo "$(GREEN)Running tests...$(NC)"
	@go test -v -race -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Coverage report generated: coverage.html$(NC)"

test-verbose: ## Run tests with verbose output
	@go test -v -race ./...

test-unit: ## Run only unit tests
	@go test -v -short -race ./...

coverage: ## Display test coverage percentage
	@go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | awk '/total:/{print $$0}'

coverage-core: ## Coverage for core business/API packages (target: >=90%)
	@go test ./test ./internal/service ./internal/handler ./internal/model ./internal/model/dto ./internal/logger \
		-coverpkg=./internal/service,./internal/handler,./internal/model,./internal/model/dto,./internal/logger \
		-coverprofile=coverage-core.out
	@go tool cover -func=coverage-core.out | awk '/total:/{print $$0}'

coverage-html: ## Generate HTML coverage report
	@go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out

## Linting & Code Quality
lint: ## Run golangci-lint
	@echo "$(GREEN)Running linter...$(NC)"
	@which golangci-lint > /dev/null || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@golangci-lint run --deadline=5m

fmt: ## Format code using gofmt
	@echo "$(GREEN)Formatting code...$(NC)"
	@go fmt ./...

vet: ## Run go vet
	@echo "$(GREEN)Running go vet...$(NC)"
	@go vet ./...

## Cleanup
clean: ## Clean build artifacts and generated files
	@echo "$(GREEN)Cleaning up...$(NC)"
	@rm -f bin/wallet-service coverage.out coverage.html
	@go clean

tidy: ## Run go mod tidy
	@echo "$(GREEN)Tidying go modules...$(NC)"
	@go mod tidy

## Development
deps: ## Download dependencies
	@echo "$(GREEN)Downloading dependencies...$(NC)"
	@go mod download

dev-setup: deps docker-up ## Setup development environment with docker
	@echo "$(GREEN)Development environment ready!$(NC)"

dev-teardown: docker-down clean ## Teardown development environment
	@echo "$(GREEN)Development environment cleaned up!$(NC)"
