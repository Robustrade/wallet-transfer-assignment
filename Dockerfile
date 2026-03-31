# Build Stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install necessary build tools
RUN apk add --no-cache git

# Copy go mod files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the main application
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

# Build the migration runner (optional but good for initialize)
RUN CGO_ENABLED=0 GOOS=linux go build -o migrate ./scripts/migrate.go

# Final minimal runner stage
FROM alpine:latest

WORKDIR /app

# Copy binaries from builder
COPY --from=builder /app/server .
COPY --from=builder /app/migrate .
COPY --from=builder /app/db /app/db

# Default Port
EXPOSE 8080

RUN chmod +x ./server ./migrate

CMD ["./server"]
