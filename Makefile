.PHONY: help tidy fmt vet test test-race cover lint build run migrate docker-up docker-down check

GO ?= go
BINARY ?= bin/wallet-service
CONFIG ?= config.yaml

help:
	@echo "Targets: tidy fmt vet test test-race cover lint build run migrate docker-up docker-down check"

tidy:
	$(GO) mod tidy

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

cover:
	$(GO) test -race -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out

lint:
	golangci-lint run ./...

build:
	$(GO) build -trimpath -o $(BINARY) ./cmd/wallet-service

run: build
	WALLET_CONFIG=$(CONFIG) ./$(BINARY)

migrate:
	WALLET_CONFIG=$(CONFIG) $(GO) run ./cmd/wallet-service migrate

docker-up:
	docker compose up --build

docker-down:
	docker compose down

check: fmt vet test-race
