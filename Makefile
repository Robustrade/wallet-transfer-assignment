# Loads DATABASE_URL / PORT from .env if present (gitignored).
-include .env
export

BIN_DIR       := $(CURDIR)/bin
GOOSE         := $(BIN_DIR)/goose
GOOSE_VER     := v3.27.1
GOLANGCI      := $(BIN_DIR)/golangci-lint
GOLANGCI_VER  := v1.64.8
MIGRATIONS    := migrations

# goose reads these from the environment, so the DSN never appears on the
# command line (no shell word-splitting on '&', no credentials in `ps`/logs).
# Make exports variable values directly to the child env (no shell parsing).
# dotenv parsers strip surrounding quotes; Make keeps them literally. Strip one
# matched layer of single/double quotes so a quoted .env value works for goose,
# `make run`, and `make itest` alike. (DSN has no spaces, so patsubst treats it
# as one word; unquoted values pass through unchanged.)
DATABASE_URL := $(patsubst "%",%,$(patsubst '%',%,$(DATABASE_URL)))
GOOSE_DRIVER := postgres
GOOSE_DBSTRING := $(DATABASE_URL)

.DEFAULT_GOAL := help

.PHONY: help
help: ## List available targets
	@grep -hE '^[a-zA-Z0-9_.-]+:.*?## ' $(MAKEFILE_LIST) | sort \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n",$$1,$$2}'

# ---- Pinned dev tools (installed into ./bin, gitignored) ----
$(GOOSE):
	GOBIN=$(BIN_DIR) go install github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VER)

$(GOLANGCI):
	GOBIN=$(BIN_DIR) go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_VER)

.PHONY: tools
tools: $(GOOSE) $(GOLANGCI) ## Install pinned goose + golangci-lint into ./bin

# ---- Build / run ----
.PHONY: build
build: ## Build the server binary into ./bin
	go build -o $(BIN_DIR)/server ./cmd/server

.PHONY: run
run: ## Run the HTTP server
	go run ./cmd/server

# ---- Quality ----
# --cached --others --exclude-standard = tracked + untracked, minus gitignored
# (so pre-commit code is checked, but ./bin and other ignored paths are not).
GO_FILES = $(shell git ls-files --cached --others --exclude-standard '*.go')

.PHONY: fmt
fmt: ## gofmt -w all Go files (tracked + untracked, excluding gitignored)
	@if [ -n "$(GO_FILES)" ]; then gofmt -w $(GO_FILES); else echo "no go files yet"; fi

.PHONY: fmt-check
fmt-check: ## Fail if any Go file is not gofmt-clean
	@if [ -z "$(GO_FILES)" ]; then echo "no go files yet"; exit 0; fi; \
	bad="$$(gofmt -l $(GO_FILES))"; \
	if [ -n "$$bad" ]; then echo "gofmt needed:"; echo "$$bad"; exit 1; fi; \
	echo "gofmt clean"

.PHONY: lint
lint: $(GOLANGCI) ## Run golangci-lint (pinned, matches CI)
	$(GOLANGCI) run ./...

.PHONY: tidy
tidy: ## go mod tidy
	go mod tidy

# ---- Tests ----
.PHONY: test
test: ## Unit tests only (DATABASE_URL blanked so integration tests self-skip)
	DATABASE_URL= go test ./...

.PHONY: itest
itest: ## Integration + concurrency tests with race detector (needs DATABASE_URL)
	CGO_ENABLED=1 go test -race -count=1 ./...

# ---- Migrations (goose, against $DATABASE_URL) ----
.PHONY: migrate-up
migrate-up: $(GOOSE) ## Apply all pending migrations
	@$(GOOSE) -dir $(MIGRATIONS) up

.PHONY: migrate-down
migrate-down: $(GOOSE) ## Roll back the most recent migration
	@$(GOOSE) -dir $(MIGRATIONS) down

.PHONY: migrate-status
migrate-status: $(GOOSE) ## Show applied/pending migrations
	@$(GOOSE) -dir $(MIGRATIONS) status

.PHONY: migrate-create
migrate-create: $(GOOSE) ## Create a migration: make migrate-create name=add_x
	$(GOOSE) -dir $(MIGRATIONS) create $(name) sql
