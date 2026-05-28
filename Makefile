SHELL := /bin/bash

DATABASE_URL ?= postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable
HTTP_ADDR    ?= :8080
DEBUG_ADDR   ?= 127.0.0.1:6060   # pprof admin listener; set to empty to disable

.PHONY: help
help:
	@echo "Targets:"
	@echo "  db-up           Start Postgres via docker compose"
	@echo "  db-down         Stop Postgres"
	@echo "  run             Run the HTTP server"
	@echo "  build           Build the server binary"
	@echo "  test            Run unit tests"
	@echo "  test-int        Run integration tests (needs DATABASE_URL -- see \`make db-up\`)"
	@echo "  test-all        Run unit + integration tests"
	@echo "  stress          Run stress tests (1k goroutines hot-wallet, 10k transfers, ~35s)"
	@echo "  bench           Run Go benchmarks (needs DATABASE_URL, ~90s total)"
	@echo "  load            Run k6 HTTP load test against \`make run\` (requires k6)"
	@echo "  lint            Run golangci-lint (default tag)"
	@echo "  lint-int        Run golangci-lint with integration build tag"
	@echo "  fmt-check       Verify gofmt compliance"
	@echo "  tidy            go mod tidy"
	@echo "  pprof-heap      Open the heap profile (web UI on :8081)"
	@echo "  pprof-cpu       Capture a 30s CPU profile, open it (web UI on :8081)"
	@echo "  pprof-goroutine Open the goroutine profile (web UI on :8081)"
	@echo "  pprof-trace     Capture a 5s execution trace and open go tool trace"

.PHONY: db-up
db-up:
	docker compose up -d postgres

.PHONY: db-down
db-down:
	docker compose down

.PHONY: build
build:
	go build -o bin/server ./cmd/server

.PHONY: run
run:
	DATABASE_URL=$(DATABASE_URL) HTTP_ADDR=$(HTTP_ADDR) DEBUG_ADDR=$(DEBUG_ADDR) go run ./cmd/server

# pprof helpers - the server must be running with DEBUG_ADDR set.
.PHONY: pprof-heap pprof-cpu pprof-goroutine pprof-trace

pprof-heap:
	go tool pprof -http=:8081 http://$(DEBUG_ADDR)/debug/pprof/heap

pprof-cpu:
	go tool pprof -http=:8081 http://$(DEBUG_ADDR)/debug/pprof/profile?seconds=30

pprof-goroutine:
	go tool pprof -http=:8081 http://$(DEBUG_ADDR)/debug/pprof/goroutine

pprof-trace:
	curl -s -o trace.out http://$(DEBUG_ADDR)/debug/pprof/trace?seconds=5 && go tool trace trace.out

.PHONY: test
test:
	go test -race -cover ./...

.PHONY: test-int
# `-p 1` forces one test binary at a time. Cross-package parallelism
# would let two packages TRUNCATE the same shared Postgres mid-test
# (testdb.Reset is per-process but the DB is shared across processes).
test-int:
	DATABASE_URL=$(DATABASE_URL) \
	  go test -tags=integration -race -count=1 -timeout=300s -p 1 ./...

.PHONY: stress
stress:
	DATABASE_URL=$(DATABASE_URL) \
	  go test -tags='integration stress' -race -count=1 -timeout=600s \
	    -run='Stress|NoGoroutineLeak' -v ./internal/service/...

.PHONY: bench
bench:
	DATABASE_URL=$(DATABASE_URL) \
	  go test -tags=integration -bench=. -benchmem -benchtime=3s \
	    -run='^$$' -timeout=300s ./internal/service/...

# HTTP-level load test. Requires `make db-up` and `make run` already
# running in another terminal. Override RPS, DURATION, WALLETS, or
# BASE_URL via env to explore the service's response curve.
.PHONY: load
load:
	k6 run \
	    --summary-trend-stats="avg,min,med,max,p(95),p(99)" \
	    loadtest/transfer.js

.PHONY: test-all
test-all: test test-int

.PHONY: lint lint-int
lint:
	golangci-lint run ./...
lint-int:
	golangci-lint run --build-tags=integration ./...

.PHONY: fmt-check
fmt-check:
	test -z "$$(gofmt -l .)"

.PHONY: tidy
tidy:
	go mod tidy
