# ==============================================================================
# Wallet Transfer Service - Multi-Stage Dockerfile
# ==============================================================================
# 
# Builder Stage:
# - Uses Go 1.26 on Alpine for a small, secure build environment.
# - Compiles the static binary for the wallet-server.
#
# Final Stage:
# - Uses a minimal Alpine 3.19 base.
# - Includes only the binary and necessary database migrations.
# - Runs as a lightweight container (~20MB).
# ==============================================================================

# Build stage using Go 1.26
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Install essential build tools
RUN apk add --no-cache git ca-certificates

# Download dependencies separately to leverage Docker cache
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the application binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o wallet-server ./cmd/api

# Final stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy the binary and migrations from the builder stage
COPY --from=builder /app/wallet-server .
COPY --from=builder /app/migrations ./migrations

# Expose the application port
EXPOSE 8080

ENTRYPOINT ["./wallet-server"]