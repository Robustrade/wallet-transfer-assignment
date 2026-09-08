DATABASE_URL ?= postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable
TEST_DATABASE_URL ?= $(DATABASE_URL)

.PHONY: run test test-race vet fmt migrate-up migrate-down

run:
	DATABASE_URL=$(DATABASE_URL) go run ./cmd/server

test:
	go test ./...

vet:
	go vet ./...

test-race:
	TEST_DATABASE_URL=$(TEST_DATABASE_URL) go test -race ./...

migrate-up:
	@for file in migrations/*_up.sql; do PGPASSWORD=wallet psql -U wallet -d wallet -h localhost -v ON_ERROR_STOP=1 -f $$file; done

migrate-down:
	@for file in $$(ls migrations/*_down.sql | sort -r); do PGPASSWORD=wallet psql -U wallet -d wallet -h localhost -v ON_ERROR_STOP=1 -f $$file; done

lint:
	go vet ./...

fmt:
	gofmt -w cmd internal