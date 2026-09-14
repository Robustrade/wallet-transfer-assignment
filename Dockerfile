FROM golang:1.26.2 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /wallet-transfer ./cmd/main.go

FROM alpine:3.22

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=builder /wallet-transfer /app/wallet-transfer
COPY --from=builder /app/db /app/db

EXPOSE 8080

ENTRYPOINT ["/app/wallet-transfer"]