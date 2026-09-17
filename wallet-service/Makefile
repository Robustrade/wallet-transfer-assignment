.PHONY: build run test lint fmt docker-up docker-down db-up db-reset

DATABASE_URL ?= postgres://wallet_user:wallet_pass@localhost:5432/wallet_db?sslmode=disable

build:
	go build -o bin/wallet-service ./cmd/server

run: build
	DATABASE_URL=$(DATABASE_URL) ./bin/wallet-service

test:
	DATABASE_URL=$(DATABASE_URL) go test ./... -v -count=1 -race

lint:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "Files not formatted:" && gofmt -l . && exit 1)

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down -v

db-up:
	docker compose up -d postgres

db-reset:
	docker compose down -v postgres
	docker compose up -d postgres
