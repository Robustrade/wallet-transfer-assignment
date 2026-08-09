..PHONY: all test coverage fmt vet build mock sqlc docker-build docker-up docker-down compose-build compose-up compose-down compose-logs clean migrate-up migrate-down

-include .env

MIGRATE ?= migrate
MIGRATE_DIR ?= database/migrations
DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@$(POSTGRES_HOST):$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable

all: test

build:
	go build ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

test:
	go test ./...

mock:
	mockgen -source=internal/repository/interface.go -destination=internal/repository/mock/interface.go -package=mock
	mockgen -source=internal/services/interface.go -destination=internal/services/mock/interface.go -package=mock_services

sqlc:
	sqlc generate

coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

docker-build:
	docker build -t wallet-transfer-assignment:local .

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

compose-build:
	docker compose build

compose-up:
	docker compose up -d

compose-down:
	docker compose down

compose-logs:
	docker compose logs -f

seed:
	docker compose exec -T db psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) < scripts/seed.sql

migrate-up:
	$(MIGRATE) -path $(MIGRATE_DIR) -database "$(DATABASE_URL)" up

migrate-down:
	$(MIGRATE) -path $(MIGRATE_DIR) -database "$(DATABASE_URL)" down

clean:
	rm -f internal/repository/mock/interface.go internal/services/mock/interface.go
