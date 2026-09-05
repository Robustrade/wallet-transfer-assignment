APP_NAME=wallet-transfer-service

DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/wallet_transfer_test?sslmode=disable
TEST_DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/wallet_transfer_test?sslmode=disable

.PHONY: help deps generate db-up db-down db-reset run test test-unit test-integration fmt tidy check

help:
	@echo "Available commands:"
	@echo "  make deps              Install/download dependencies"
	@echo "  make generate          Generate Ent code with row-lock support"
	@echo "  make db-up             Start PostgreSQL"
	@echo "  make db-down           Stop PostgreSQL"
	@echo "  make db-reset          Reset PostgreSQL data"
	@echo "  make run               Run the HTTP server"
	@echo "  make test              Run all tests"
	@echo "  make test-unit         Run unit tests"
	@echo "  make test-integration  Run PostgreSQL integration tests"
	@echo "  make fmt               Format Go code"
	@echo "  make tidy              Run go mod tidy"
	@echo "  make check             Format and run all tests"

deps:
	go mod download

generate:
	go run entgo.io/ent/cmd/ent generate --feature sql/lock ./ent/schema

db-up:
	docker compose up -d

db-down:
	docker compose down

db-reset:
	docker compose down -v
	docker compose up -d

run:
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/server

test:
	go test ./...

test-unit:
	go test ./internal/... -v

test-integration:
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test ./tests/integration/... -v

fmt:
	gofmt -w ./cmd ./internal ./ent/schema ./tests

tidy:
	go mod tidy

check: fmt test