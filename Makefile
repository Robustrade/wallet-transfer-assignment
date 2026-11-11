PROFILE_DIR := profiles
SHELL := bash
# Override when the application entry point is added elsewhere.
APP_PACKAGE ?= ./cmd/server
# Profiling targets one package; override with a package containing benchmarks.
PROFILE_PACKAGE ?= ./internal/service

.PHONY: all check run fmt fmt-check lint vet test profile clean

# Default target when running `make`
all: check

# Check without changing source files. Profiling is an explicit separate action.
check: fmt-check lint vet test

run:
	go run $(APP_PACKAGE)

fmt:
	golangci-lint fmt

fmt-check:
	@diff_output="$$(golangci-lint fmt --diff)" || exit $$?; \
	if [ -n "$$diff_output" ]; then printf '%s\n' "$$diff_output"; exit 1; fi

lint:
	golangci-lint run

vet:
	go vet ./...

$(PROFILE_DIR):
	mkdir -p $(PROFILE_DIR)

test: | $(PROFILE_DIR)
	go test -race -v -coverprofile=$(PROFILE_DIR)/coverage.out ./...

profile: | $(PROFILE_DIR)
	go test -run=^$$ -bench=. -benchmem \
		-cpuprofile=$(PROFILE_DIR)/cpu.prof \
		-memprofile=$(PROFILE_DIR)/mem.prof $(PROFILE_PACKAGE)

clean:
	rm -rf $(PROFILE_DIR)
	go clean -testcache
