DATABASE_URL ?= postgres://wallet:wallet@127.0.0.1:5432/wallet?sslmode=disable

.PHONY: db-up db-down run test test-integration fmt lint check

db-up:
	docker compose up -d --wait

db-down:
	docker compose down

run:
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/server

test:
	go test ./... -race -count=1

test-integration: db-up
	DATABASE_URL="$(DATABASE_URL)" go test ./... -race -count=1

fmt:
	gofmt -w .

lint:
	golangci-lint run ./...

check: fmt lint test-integration
