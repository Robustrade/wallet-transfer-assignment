.PHONY: run test test-integration lint fmt fmt-check migrate-up migrate-down seed up down

run:
	go run ./cmd/server

test:
	go test ./... -race -cover

lint:
	golangci-lint run ./...

fmt:
	go fmt ./...

fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt'd:"; echo "$$unformatted"; exit 1; \
	fi

up:
	docker compose up -d --wait

down:
	docker compose down

migrate-up:
	docker compose exec -T postgres psql -U postgres -d wallet_transfer -v ON_ERROR_STOP=1 -f - < migrations/0001_init.up.sql

migrate-down:
	docker compose exec -T postgres psql -U postgres -d wallet_transfer -v ON_ERROR_STOP=1 -f - < migrations/0001_init.down.sql

seed:
	docker compose exec -T postgres psql -U postgres -d wallet_transfer -v ON_ERROR_STOP=1 -f - < scripts/seed.sql

test-integration:
	DATABASE_URL="postgres://postgres:postgres@localhost:5432/wallet_transfer?sslmode=disable" go test ./... -cover
