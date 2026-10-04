FROM golang:1.25-alpine AS build

WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/wallet-service ./cmd/wallet-service

FROM alpine:3.20

RUN apk add --no-cache ca-certificates wget \
    && adduser -D -u 65532 app
WORKDIR /app
COPY --from=build /out/wallet-service /usr/local/bin/wallet-service
COPY config.yaml /app/config.yaml
USER app
EXPOSE 8080
ENV WALLET_CONFIG=/app/config.yaml
ENTRYPOINT ["wallet-service"]
