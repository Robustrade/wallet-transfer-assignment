# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /wallet-transfer ./cmd/server

# Final stage
FROM alpine:latest

WORKDIR /

COPY --from=builder /wallet-transfer /wallet-transfer

EXPOSE 8080

ENTRYPOINT ["/wallet-transfer"]