.PHONY: all build run test test-integration test-all fmt vet lint docker-up docker-down migrate seed clean

all: fmt vet build test

build:
	go build -o bin/api.exe ./cmd/api

run:
	go run ./cmd/api

test:
	go test -v -race ./tests/unit/...

test-integration:
	go test -v -race -count=1 ./tests/integration/...

test-all:
	go test -v -race ./...

fmt:
	gofmt -s -w .

vet:
	go vet ./...

lint: fmt vet

docker-up:
	docker compose up -d

docker-down:
	docker compose down -v

migrate:
	docker compose exec -T postgres psql -U postgres -d wallet_db -f /docker-entrypoint-initdb.d/01_create_tables.sql

seed:
	docker compose exec -T postgres psql -U postgres -d wallet_db -f /docker-entrypoint-initdb.d/02_seed.sql

clean:
	rm -rf bin/ coverage/ *.test
