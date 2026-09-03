TEST_DATABASE_URL ?= postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable

.PHONY: build run test test-postgres lint fmt fmt-check

build:
	go build ./...

run:
	go run ./cmd/walletd

# Runs against SQLite only, which needs no services. This is what CI runs.
test:
	go test ./... -race -cover

# Adds the PostgreSQL backend. Start it first with: docker compose up -d postgres
test-postgres:
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test ./... -race -cover

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

fmt-check:
	test -z "$$(gofmt -l .)"
