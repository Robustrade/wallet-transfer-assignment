GO ?= go
# Do not auto-download Go toolchains (fixes "toolchain not available" on Windows).
export GOTOOLCHAIN ?= local

MIGRATE_VERSION ?= v4.18.3
MIGRATE = $(GO) run -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@$(MIGRATE_VERSION)
DATABASE_URL ?= postgres://wallet:wallet@localhost:5433/wallet_transfer?sslmode=disable

.PHONY: db-up db-down db-migrate db-migrate-down db-reset install-migrate run test test-coverage check-go

check-go:
	@$(GO) version

db-up:
	docker compose up -d postgres

db-down:
	docker compose down

db-migrate:
	$(MIGRATE) -path migrations -database "$(DATABASE_URL)" up

db-migrate-down:
	$(MIGRATE) -path migrations -database "$(DATABASE_URL)" down 1

db-reset: db-migrate-down db-migrate

install-migrate:
	$(GO) install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@$(MIGRATE_VERSION)

run:
	$(GO) run ./cmd/server

test: check-go
	$(GO) mod tidy
	$(GO) test ./...

test-coverage: check-go
	$(GO) mod tidy
	$(GO) test ./... -coverprofile=coverage.out
	$(GO) tool cover -func=coverage.out
