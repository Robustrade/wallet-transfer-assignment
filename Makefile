.PHONY: build run test test-unit test-integration docker-up docker-down test-docker lint fmt fmt-check migrate clean

APP_NAME := wallet-transfer
DATABASE_URL ?= postgres://wallet:wallet_secret@localhost:5432/wallet_transfer?sslmode=disable

build:
	go build -o bin/$(APP_NAME) ./cmd/server

run: build
	DATABASE_URL=$(DATABASE_URL) ./bin/$(APP_NAME)

test:
	go test ./internal/... -v -count=1

test-unit:
	go test ./internal/domain/... -v -count=1

test-integration:
	DATABASE_URL=$(DATABASE_URL) go test ./tests/integration/... -v -count=1 -tags=integration

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down -v

test-docker:
	@exit_code=0; \
	docker compose -f docker-compose.test.yml up --build --abort-on-container-exit --exit-code-from test-runner || exit_code=$$?; \
	docker compose -f docker-compose.test.yml down -v; \
	exit $$exit_code

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "Files not formatted:" && gofmt -l . && exit 1)

migrate:
	psql $(DATABASE_URL) -f migrations/001_init.sql

clean:
	rm -rf bin/
