include .env
export

DATABASE_URL=postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=$(DB_SSLMODE)

.PHONY: migrate-up migrate-down migrate-version test test-integration lint coverage coverage-integration sonar-scan docker-up docker-down docker-reset docker-logs

migrate-up:
	migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path migrations -database "$(DATABASE_URL)" down 1

migrate-version:
	migrate -path migrations -database "$(DATABASE_URL)" version

# Unit tests: domain, handler, service (in-memory fakes). No database needed.
test:
	go test -race ./...

# Unit-test coverage report (coverage.out) for SonarQube and `go tool cover`.
# Only packages that have tests are run: `go test -cover` on test-less packages trips over
# toolchain mismatches on some setups ("compile: version ... does not match go tool version").
TESTED_PKGS = $$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...)
TESTED_PKGS_INTEGRATION = $$(go list -tags integration -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...)

coverage:
	go test -race -covermode=atomic -coverpkg=./internal/... -coverprofile=coverage.out $(TESTED_PKGS)

# Same, plus the PostgreSQL integration tests (needs the .env database): covers repositories.
coverage-integration:
	@unset APP_PORT APP_ENV DB_HOST DB_PORT DB_USER DB_PASSWORD DB_NAME DB_SSLMODE LOG_LEVEL; \
	go test -tags integration -race -covermode=atomic -coverpkg=./internal/... -coverprofile=coverage.out -p 1 $(TESTED_PKGS_INTEGRATION)

# Static analysis (config: .golangci.yml, same version as CI):
#   go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
lint:
	golangci-lint run ./...

# Integration/concurrency tests against PostgreSQL from .env (migrations applied).
test-integration:
	@# make exports .env values with their quotes; unset so the test loads .env itself.
	@unset APP_PORT APP_ENV DB_HOST DB_PORT DB_USER DB_PASSWORD DB_NAME DB_SSLMODE LOG_LEVEL; \
	go test -tags integration -race -count=1 ./internal/feature/wallet/integration/

# --- Docker: Postgres + migrations + seed data + API, all in one command ---------
# make exports .env values with their quotes, which would leak into docker compose;
# unset them so compose reads .env itself.
COMPOSE = unset APP_PORT APP_ENV DB_HOST DB_PORT DB_USER DB_PASSWORD DB_NAME DB_SSLMODE LOG_LEVEL; docker compose

docker-up:
	@$(COMPOSE) up -d --build --wait

docker-down:
	@$(COMPOSE) down

# Wipe the database volume and start again with a fresh DB, migrations and seed data.
docker-reset:
	@$(COMPOSE) down -v
	@$(COMPOSE) up -d --build --wait

docker-logs:
	@$(COMPOSE) logs -f api

# --- SonarQube Cloud ---------------------------------------------------------------
# Runs the tests with coverage (integration tests too with COVERAGE_TARGET=coverage-integration),
# then analyses the project with the SonarScanner CLI and uploads it to sonarcloud.io.
#   SONAR_TOKEN=<token> SONAR_ORGANIZATION=<organization-key> make sonar-scan
# The project key is read from sonar-project.properties. Note: this uploads the source
# code and coverage to sonarcloud.io.
COVERAGE_TARGET ?= coverage
sonar-scan:
	@test -n "$$SONAR_TOKEN" -a -n "$$SONAR_ORGANIZATION" || { echo "Set SONAR_TOKEN and SONAR_ORGANIZATION (SonarQube Cloud: My Account > Security, and your organization key)"; exit 1; }
	@$(MAKE) $(COVERAGE_TARGET)
	@$(COMPOSE) --profile sonar run --rm sonar-scanner
