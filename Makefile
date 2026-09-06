.PHONY: all build run test test-unit test-integration test-race fmt lint docker-up docker-down migrate-up migrate-down clean

APP_NAME := wallet-transfer-service
MAIN_PATH := ./cmd/server
DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/wallet_db?sslmode=disable
MIGRATE := go run github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1

all: fmt lint test build

build:
	go build -o bin/$(APP_NAME) $(MAIN_PATH)

run:
	go run $(MAIN_PATH)

test: test-unit test-integration

test-unit:
	go test -v ./tests/unit/...

test-integration:
	go test -v ./tests/integration/...

test-race:
	go test -race -v ./tests/...

fmt:
	gofmt -w -s .

lint:
	golangci-lint run ./...

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down -v

migrate-up:
	$(MIGRATE) -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	$(MIGRATE) -path migrations -database "$(DATABASE_URL)" down -all

clean:
	rm -rf bin/

