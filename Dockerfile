FROM golang:1.25 AS builder

WORKDIR /app

RUN apt-get update && \
    apt-get install -y gcc libc6-dev git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 go build -o /app/wallet ./cmd

FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y ca-certificates sqlite3 && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /app/wallet .

RUN mkdir -p /app/data

ENV DB_PATH=/app/data/wallet.db

EXPOSE 8080

ENTRYPOINT ["./wallet"]