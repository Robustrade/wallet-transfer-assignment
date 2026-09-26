DATABASE_URL ?= postgres://wallet:wallet@localhost:5433/wallet?sslmode=disable
# Override with COMPOSE="docker compose" if the compose plugin is installed.
COMPOSE ?= docker-compose

.PHONY: db-up db-down db-reset migrate seed run test fmt vet tidy

db-up:
	$(COMPOSE) up -d --wait db

db-down:
	$(COMPOSE) down

# Destroys all local data, recreates the database, and reruns migrations + seed.
db-reset:
	$(COMPOSE) down -v
	$(COMPOSE) up -d --wait db
	$(MAKE) migrate seed

migrate:
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/migrate

seed:
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/seed

run:
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/api

# Tests run against wallet_test (auto-created); override with TEST_DATABASE_URL.
test:
	go test ./... -count=1

fmt:
	gofmt -l -w .

vet:
	go vet ./...

tidy:
	go mod tidy
