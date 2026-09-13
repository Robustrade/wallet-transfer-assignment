.PHONY: sqlc generate test run

sqlc:
	sqlc generate

run :
	go run cmd/main.go

test :
	go test ./... -v