.PHONY: all build clean test test-race lint run tidy

BIN_DIR := bin
BINARY := $(BIN_DIR)/server
MAIN_PKG := ./cmd/server

all: tidy build test

build:
	@mkdir -p $(BIN_DIR)
	go build -v -o $(BINARY) $(MAIN_PKG)

run: build
	./$(BINARY)

test:
	go test -v ./...

test-race:
	go test -v -race -count=1 ./...

lint:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)
