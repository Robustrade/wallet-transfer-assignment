# Targets mirror the CI commands (see branch-protection-checklist.md).
GOLANGCI_LINT_VERSION := v1.64.8
DATABASE_URL ?= postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable

.PHONY: up down run seed test lint fmt fmt-check

up: ## start Postgres and wait until healthy
	docker compose up -d --wait postgres

down: ## stop Postgres and remove its volume
	docker compose down -v

run: ## run the API server against the local Postgres
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/server

seed: ## insert demo wallets (run after the server has applied migrations)
	docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U wallet -d wallet < scripts/seed.sql

test: ## run all tests with the race detector (integration tests start their own Postgres container)
	go test ./... -race -cover

lint:
	GOTOOLCHAIN=go1.24.9 go run github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "files above need gofmt" && exit 1)
