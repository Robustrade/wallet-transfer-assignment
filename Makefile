.PHONY: all build run test test-unit test-integration test-race fmt lint docker-up docker-down clean

APP_NAME := wallet-transfer-service
MAIN_PATH := ./cmd/server

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

clean:
	rm -rf bin/
