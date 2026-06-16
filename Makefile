.PHONY: up down run test lint fmt migrate docker-build

# Load .env when present so local DB_* / DATABASE_URL settings apply.
ifneq (,$(wildcard .env))
include .env
export
endif

# Default: Docker Compose Postgres on host port 5433 (avoids conflict with local Postgres on 5432).
DATABASE_URL ?= postgres://wallet:wallet@localhost:5433/wallet_transfer?sslmode=disable

up:
	docker compose up -d postgres

down:
	docker compose down

run:
	go run ./cmd/server

test:
	go test ./... -race -count=1
	go test -tags=integration ./tests/integration/... -race -count=1

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)"

migrate:
	@command -v migrate >/dev/null 2>&1 || { echo "install golang-migrate: https://github.com/golang-migrate/migrate"; exit 1; }
	migrate -path internal/migrate/migrations -database "$${DATABASE_URL}" up

swagger:
	@command -v swag >/dev/null 2>&1 || { echo "install swag: go install github.com/swaggo/swag/cmd/swag@latest"; exit 1; }
	swag init -g cmd/server/main.go -o docs --parseDependency --parseInternal

docker-build:
	docker compose build
