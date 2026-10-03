TEST_DATABASE_URL ?= postgres://wallet:wallet@localhost:5432/wallet_test?sslmode=disable

.PHONY: db-up db-down db-migrate run test test-race fmt

db-up:
	docker compose up -d --wait

db-down:
	docker compose down

db-migrate:
	cat migrations/*.sql | docker exec -i wallet-postgres psql -v ON_ERROR_STOP=1 --single-transaction -U wallet -d wallet

db-reset:
	docker compose down -v
	docker compose up -d
	sleep 2
	cat migrations/*.sql | docker exec -i wallet-postgres psql -U wallet -d wallet

db-test-reset:
	docker exec wallet-postgres psql -U wallet -d postgres -c "DROP DATABASE IF EXISTS wallet_test;"
	docker exec wallet-postgres psql -U wallet -d postgres -c "CREATE DATABASE wallet_test;"

db-test-migrate: db-test-reset
	cat migrations/*.sql | docker exec -i wallet-postgres psql -v ON_ERROR_STOP=1 --single-transaction -U wallet -d wallet_test

run:
	DATABASE_URL="postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable" go run ./cmd/server

test: db-test-migrate
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test ./...

test-race: db-test-migrate
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -race ./...
fmt:
	gofmt -w cmd internal tests