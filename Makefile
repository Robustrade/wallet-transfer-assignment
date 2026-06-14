.PHONY: up down migrate migrate-down build run test lint fmt help

DATABASE_URL ?= postgres://wallet:wallet@localhost:5434/wallet?sslmode=disable
TEST_DATABASE_URL ?= postgres://wallet:wallet@localhost:5434/wallet_test?sslmode=disable

## up: start the Postgres container in the background
up:
	docker-compose up -d --wait

## down: stop and remove containers and volumes
down:
	docker-compose down -v

## migrate: apply all pending migrations
migrate:
	go run ./cmd/migrate -database "$(DATABASE_URL)" -action up

## migrate-down: roll back all migrations
migrate-down:
	go run ./cmd/migrate -database "$(DATABASE_URL)" -action down

## build: compile the server binary
build:
	go build -o bin/server ./cmd/server

## run: start the server (Postgres must be up and migrations applied)
run: build
	DATABASE_URL="$(DATABASE_URL)" ./bin/server

## test: run integration tests (Postgres must be up)
test:
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -v -race -count=1 ./...

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## fmt: format all Go source files
fmt:
	gofmt -l -w .

## help: print this message
help:
	@sed -n 's/^## //p' $(MAKEFILE_LIST)
