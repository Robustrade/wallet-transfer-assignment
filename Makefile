# =====================================================
# Wallet Transfer Assignment Makefile
# =====================================================

APP_NAME=wallet-transfer
MAIN=cmd/server/main.go

DB_URL=postgres://postgres:postgres@localhost:5432/wallet_transfer?sslmode=disable

GOOSE_DIR=sql/schema

# =====================================================
# Build
# =====================================================

.PHONY: build
build:
	go build -o bin/$(APP_NAME) $(MAIN)

# =====================================================
# Run
# =====================================================

.PHONY: run
run:
	go run $(MAIN)

# =====================================================
# Clean
# =====================================================

.PHONY: clean
clean:
	rm -rf bin
	rm -f coverage.out

# =====================================================
# Format
# =====================================================

.PHONY: fmt
fmt:
	go fmt ./...

# =====================================================
# Vet
# =====================================================

.PHONY: vet
vet:
	go vet ./...

# =====================================================
# Lint
# =====================================================

.PHONY: lint
lint:
	golangci-lint run

# =====================================================
# Generate sqlc
# =====================================================

.PHONY: sqlc
sqlc:
	sqlc generate

# =====================================================
# Goose Migration
# =====================================================

.PHONY: migrate-up
migrate-up:
	goose -dir $(GOOSE_DIR) postgres "$(DB_URL)" up

.PHONY: migrate-down
migrate-down:
	goose -dir $(GOOSE_DIR) postgres "$(DB_URL)" down

.PHONY: migrate-reset
migrate-reset:
	goose -dir $(GOOSE_DIR) postgres "$(DB_URL)" reset

.PHONY: migrate-status
migrate-status:
	goose -dir $(GOOSE_DIR) postgres "$(DB_URL)" status

# =====================================================
# Test
# =====================================================

.PHONY: test
test:
	go test ./tests/... -v

# =====================================================
# Race Test
# =====================================================

.PHONY: race
race:
	go test ./tests/... -race -v

# =====================================================
# Coverage
# =====================================================

.PHONY: cover
cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out

# =====================================================
# Benchmark
# =====================================================

.PHONY: bench
bench:
	go test -bench=. ./...

# =====================================================
# Tidy
# =====================================================

.PHONY: tidy
tidy:
	go mod tidy

# =====================================================
# Download Dependencies
# =====================================================

.PHONY: deps
deps:
	go mod download

# =====================================================
# Full CI
# =====================================================

.PHONY: ci
ci:
	go fmt ./...
	go vet ./...
	go test ./... -race -cover