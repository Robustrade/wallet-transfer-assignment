.PHONY: up down migrate migrate-down seed run test race lint fmt-check

DATABASE_URL ?= postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable
MIGRATE := go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1

up:
	docker compose up -d
	@echo "waiting for postgres..."
	@until docker compose exec -T postgres pg_isready -U wallet -d wallet_transfer >/dev/null 2>&1; do sleep 1; done

down:
	docker compose down

migrate:
	$(MIGRATE) -database "$(DATABASE_URL)" -path migrations up

migrate-down:
	$(MIGRATE) -database "$(DATABASE_URL)" -path migrations down 1

seed:
	docker compose exec -T postgres psql -U wallet -d wallet_transfer < seed/dev_seed.sql

run:
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/server

test: up migrate
	go test ./... -race -cover

lint:
	golangci-lint run ./...

fmt-check:
	test -z "$$(gofmt -l .)"
