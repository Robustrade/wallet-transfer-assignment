# syntax=docker/dockerfile:1

FROM golang:1.24 AS builder

WORKDIR /usr/wallets/app

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN CGO_ENABLED=0 go build -v -o app ./cmd/server

FROM alpine:3.22

WORKDIR /wallets
COPY --from=builder /usr/wallets/app/app .

ENTRYPOINT [ "/wallets/app" ]
