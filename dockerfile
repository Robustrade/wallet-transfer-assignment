# Build stage
FROM golang:1.25 AS builder
WORKDIR /app

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

# Build a static binary (CGO off) so it runs on a minimal base image.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o wallet .

# Run stage
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /app/wallet .
EXPOSE 8080
USER nobody
ENTRYPOINT ["./wallet"]
