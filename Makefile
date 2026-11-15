GOLANGCI_LINT_VERSION := v1.64.8

export GOTOOLCHAIN := local

.PHONY: up down reset run test lint fmt fmt-check

up:
	docker compose up -d --wait

down:
	docker compose down

reset:
	docker compose down -v

run:
	go run ./cmd/server

test:
	go test ./... -race -count=1

lint:
	docker run --rm -v "$(CURDIR)":/app -w /app golangci/golangci-lint:$(GOLANGCI_LINT_VERSION) golangci-lint run ./...

fmt:
	gofmt -w .

fmt-check:
	test -z "$$(gofmt -l .)"
