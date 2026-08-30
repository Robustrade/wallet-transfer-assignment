.PHONY: run test lint fmt

run:
	go run ./cmd/server

test:
	go test ./... -count=1

lint:
	golangci-lint run ./...

fmt:
	gofmt -w $(shell find . -name '*.go' -not -path './.git/*')

fmt-check:
	bash scripts/format-check.sh
