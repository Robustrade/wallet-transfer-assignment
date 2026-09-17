# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /wallet-service ./cmd/server

# Run stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

WORKDIR /app
COPY --from=builder /wallet-service .
COPY internal/db/migrations ./migrations

EXPOSE 8080

CMD ["./wallet-service"]
