.PHONY: up down logs migrate run fmt test integration clean

up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f

migrate:
	docker compose exec -T postgres psql -U wallet -d wallet < migrations/001_init.sql

run:
	go run ./cmd/api

fmt:
	gofmt -w ./cmd ./internal

test:
	go test ./...

integration:
	TEST_DATABASE_URL=postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable \
	go test ./internal/repository -run Test -v

clean:
	docker compose down -v