.PHONY: up down build run test lint fmt

DSN ?= postgres://postgres:postgres@localhost:5432/wallet_transfer?sslmode=disable
TEST_DSN ?= postgres://postgres:postgres@localhost:5432/wallet_transfer_test?sslmode=disable

## Start PostgreSQL via Docker Compose (detached)
up:
	docker compose up -d postgres

## Stop all Compose services
down:
	docker compose down

## Build the server binary
build:
	go build -o bin/server ./cmd/server

## Run the server locally (requires postgres to be up)
run: build
	DATABASE_URL=$(DSN) ./bin/server

## Run all tests (unit + integration)
test:
	DATABASE_URL=$(TEST_DSN) go test ./... -race -count=1 -v

## Run only unit tests (no DB required)
test/unit:
	go test ./internal/handler/... -race -count=1 -v

## Apply the schema to the test database
db/migrate/test:
	psql $(TEST_DSN) -f db/migrations/001_init.sql

## Apply the schema to the dev database
db/migrate:
	psql $(DSN) -f db/migrations/001_init.sql

## Run golangci-lint
lint:
	golangci-lint run ./...

## Check formatting
fmt/check:
	test -z "$$(gofmt -l .)"

## Auto-format
fmt:
	gofmt -w .
