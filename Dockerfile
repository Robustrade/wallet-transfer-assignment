# ── lint tools (golangci-lint v2 official image) ──────────────────────────────
FROM golangci/golangci-lint:v2.12.2-alpine AS lint-tools

# ── dev / quality stage (fmt · lint · test · build) ──────────────────────────
FROM golang:1.25-alpine AS dev

# gcc + musl-dev are required for CGO_ENABLED=1 (race detector)
RUN apk add --no-cache git gcc musl-dev

# gofmt ships with the Go toolchain; copy golangci-lint from the official image
COPY --from=lint-tools /usr/bin/golangci-lint /usr/local/bin/golangci-lint

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

# ── binary builder ────────────────────────────────────────────────────────────
FROM dev AS builder

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o wallet-service ./cmd/server

# ── minimal runtime image ─────────────────────────────────────────────────────
FROM alpine:3.21 AS runtime

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/wallet-service .
COPY --from=builder /app/migrations ./migrations

EXPOSE 8080

CMD ["./wallet-service"]
