.PHONY: run build test test-race lint db-up db-down migrate-up migrate-down test-coverage check-test-coverage-html deploy

DATABASE_URL ?= postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable
COVERAGE_THRESHOLD := 60
COVERAGE_OUT := coverage/coverage.out
COVERAGE_EXCLUDE := /router|/testutils|/constants
COVERAGE_PKGS := $(shell go list ./... | grep -Ev '$(COVERAGE_EXCLUDE)')

run:
	go run ./cmd/server

test:
	go test ./... -v

test-race:
	go test ./... -race -v

test-coverage:
	@mkdir -p coverage
	go test $(COVERAGE_PKGS) -coverprofile=$(COVERAGE_OUT)
	@coverage=$$(go tool cover -func=$(COVERAGE_OUT) | grep '^total:' | awk '{print substr($$3, 1, length($$3)-1)}'); \
	echo "Total coverage: $$coverage%"; \
	if awk "BEGIN {exit !($$coverage < $(COVERAGE_THRESHOLD))}"; then \
		echo "Coverage $$coverage% is below the required $(COVERAGE_THRESHOLD)% threshold."; \
		exit 1; \
	fi

deploy: test-coverage build
	@echo "Coverage threshold met, deploying..."
	# actual deploy steps go here

db-up:
	docker compose up -d postgres

db-down:
	docker compose down

migrate-up:
	migrate -database "$(DATABASE_URL)" -path migrations up

migrate-down:
	migrate -database "$(DATABASE_URL)" -path migrations down 1

build: migrate-up
	mockery
	make test
	go build -o bin/server ./cmd/server

lint:
	golangci-lint run --timeout 5m

check-test-coverage-html:
	@mkdir -p coverage
	go test $(COVERAGE_PKGS) -coverprofile=$(COVERAGE_OUT)
	go tool cover -html=$(COVERAGE_OUT) -o coverage/coverage.html