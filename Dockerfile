# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/walletd ./cmd/walletd

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && \
    addgroup -S walletd && adduser -S walletd -G walletd
COPY --from=build /out/walletd /usr/local/bin/walletd
USER walletd
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/walletd"]
