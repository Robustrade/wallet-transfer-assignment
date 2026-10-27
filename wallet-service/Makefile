.PHONY: db-up db-down migrate-up migrate-down seed run test check

MIGRATE = go run -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.20.1

db-up:
	docker compose up -d --wait db

db-down:
	docker compose down

migrate-up:
	@test -n "$$DATABASE_URL" || (echo 'DATABASE_URL is required' >&2; exit 1)
	@$(MIGRATE) -path migrations -database "$$DATABASE_URL" up

# Removes schema and data: use only on a disposable database.
migrate-down:
	@test -n "$$DATABASE_URL" || (echo 'DATABASE_URL is required' >&2; exit 1)
	@$(MIGRATE) -path migrations -database "$$DATABASE_URL" down 1

seed:
	docker compose exec -T db psql -U wallet -d wallet -v ON_ERROR_STOP=1 < seeds/wallets.sql

run:
	go run ./cmd/api

test:
	@test -n "$$TEST_DATABASE_URL" || (echo 'TEST_DATABASE_URL is required for database tests' >&2; exit 1)
	go test -race ./...

check:
	@test -z "$$(gofmt -l .)" || (echo 'Run gofmt -w .' >&2; exit 1)
	go vet ./...
	go build ./...
	$(MAKE) test
