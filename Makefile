# Wallet Transfer Service — developer tasks
#
# Requires: go >= 1.24, docker (for local postgres + integration tests),
#           golang-migrate CLI for the migrate-* targets (optional — the server
#           also applies embedded migrations on startup).

SHELL := /bin/bash

DATABASE_URL      ?= postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable
TEST_DATABASE_URL ?= $(DATABASE_URL)
MIGRATIONS_DIR    := internal/repository/migrations

export DATABASE_URL
export TEST_DATABASE_URL

.DEFAULT_GOAL := help

## help: list available targets
.PHONY: help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | awk -F': ' '{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## build: compile the server binary into ./bin
.PHONY: build
build:
	go build -o bin/server ./cmd/server

## run: run the server locally against $DATABASE_URL
.PHONY: run
run:
	go run ./cmd/server

## fmt: format all Go code
.PHONY: fmt
fmt:
	gofmt -w .

## fmt-check: fail if any Go file is not gofmt-clean (CI FORMAT_CHECK_CMD)
.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "not gofmt-clean:"; echo "$$unformatted"; exit 1; fi

## vet: go vet
.PHONY: vet
vet:
	go vet ./...

## lint: golangci-lint (CI LINT_CMD)
.PHONY: lint
lint:
	golangci-lint run ./...

## test: unit tests with race detector and coverage (CI TEST_CMD)
.PHONY: test
test:
	mkdir -p coverage
	go test ./... -race -count=1 -coverpkg=./... -coverprofile=coverage/cover.out

## test-integration: also run //go:build integration tests against $TEST_DATABASE_URL
.PHONY: test-integration
test-integration:
	mkdir -p coverage
	go test ./... -race -cover -count=1 -tags=integration -coverprofile=coverage/cover.out

## cover: show total coverage from the last test run
.PHONY: cover
cover: test
	go tool cover -func=coverage/cover.out | tail -1

## db-up: start local postgres
.PHONY: db-up
db-up:
	docker compose up -d postgres

## db-down: stop local postgres and drop its volume
.PHONY: db-down
db-down:
	docker compose down -v

## seed: load development wallet fixtures (no psql required)
.PHONY: seed
seed:
	go run ./cmd/seeddev

## migrate-up: apply all migrations with the golang-migrate CLI
.PHONY: migrate-up
migrate-up:
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" up

## migrate-down: roll back the last migration
.PHONY: migrate-down
migrate-down:
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" down 1
