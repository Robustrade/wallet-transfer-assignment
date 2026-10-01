.PHONY: test lint fmt fmt-check

test:
	go test ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .

fmt-check:
	test -z "$$(gofmt -l .)"
