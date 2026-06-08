BINARY     := wallet-transfer
DB_URL     := postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable
MIGRATE    := go run github.com/golang-migrate/migrate/v4/cmd/migrate@latest

.PHONY: up down migrate-up migrate-down test test-integration lint fmt build

up:
	docker compose up -d

down:
	docker compose down

migrate-up:
	$(MIGRATE) -path migrations -database "$(DB_URL)" up

migrate-down:
	$(MIGRATE) -path migrations -database "$(DB_URL)" down

build:
	go build -o bin/$(BINARY) ./cmd/server

test:
	go test -race -cover ./internal/...

test-integration:
	go test -race -cover -timeout 120s ./tests/integration/...

lint:
	golangci-lint run ./...

fmt:
	gofmt -l -w .
