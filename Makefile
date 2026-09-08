.PHONY: up down migrate test test-integration test-race run build

DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/wallettransfer?sslmode=disable
TEST_DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/wallettransfer_test?sslmode=disable

export DATABASE_URL
export TEST_DATABASE_URL

## Start local Postgres (via docker-compose) with schema auto-applied
up:
	docker compose up -d

down:
	docker compose down -v

## Apply migrations manually (docker-entrypoint-initdb.d only runs on first init)
migrate:
	psql "$(DATABASE_URL)" -f migrations/001_init.sql

## Fast unit tests only (no database needed)
test:
	go test ./...

## Full integration tests against a real Postgres (requires TEST_DATABASE_URL or local `make up`)
test-integration:
	go test -tags=integration ./internal/service/... -run Integration -v

## Same as above, with the race detector enabled
test-race:
	go test -tags=integration -race ./internal/service/... -run Integration -v

run:
	go run ./cmd/server

build:
	go build -o bin/server.exe ./cmd/server