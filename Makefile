# Default Environment Configurations
DATABASE_URL ?= postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable
PORT ?= 8080

# --- OS Detection ---
# Detects if running on Windows to automatically handle execution environment
ifeq ($(OS),Windows_NT)
    # Forces Make to use Bash or PowerShell syntax if available, 
    # but we handle commands cross-platform below using standard Docker primitives.
    ENV_CMD = cmd /c "set PORT=$(PORT)&& set DATABASE_URL=$(DATABASE_URL)&&"
else
    ENV_CMD = PORT=$(PORT) DATABASE_URL="$(DATABASE_URL)"
endif

.PHONY: run test test-integration fmt fmt-check lint migrate-up migrate-down check-invariants

# Runs the server locally with correctly injected environment variables
run:
	$(ENV_CMD) go run ./cmd/server

test:
	go test ./...

test-integration:
	go test -tags=integration ./...

# Simplified find command to work natively across standard environments
fmt:
	go fmt ./...

fmt-check:
	@echo "Checking formatting..."
	@test -z "$$(gofmt -l .)" || (echo "Formatting errors found. Run 'make fmt'"; exit 1)

lint:
	golangci-lint run ./...

# Routes the migration scripts through Docker Compose directly so you do not need local psql installed
migrate-up:
ifeq ($(OS),Windows_NT)
	powershell -Command "Get-Content migrations/001_init.sql -Raw | docker compose exec -T postgres psql -U wallet -d wallet_transfer"
	powershell -Command "Get-Content migrations/002_seed.sql -Raw | docker compose exec -T postgres psql -U wallet -d wallet_transfer"
else
	docker compose exec -T postgres psql -U wallet -d wallet_transfer < migrations/001_init.sql
	docker compose exec -T postgres psql -U wallet -d wallet_transfer < migrations/002_seed.sql
endif

# Rolls back the database schema via Docker Compose
migrate-down:
ifeq ($(OS),Windows_NT)
	powershell -Command "Get-Content migrations/999_down.sql -Raw | docker compose exec -T postgres psql -U wallet -d wallet_transfer"
else
	docker compose exec -T postgres psql -U wallet -d wallet_transfer < migrations/999_down.sql
endif

check-invariants:
	$(ENV_CMD) go run ./cmd/check-invariants
