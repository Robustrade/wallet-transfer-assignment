FROM golang:1.24 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o wallet-service ./cmd/server

FROM alpine:latest

WORKDIR /root/

COPY --from=builder /app/wallet-service .

EXPOSE 8080

CMD ["./wallet-service"]