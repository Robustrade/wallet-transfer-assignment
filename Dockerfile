FROM golang:1.24-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/wallet-service ./cmd/server

FROM alpine:3.20

RUN apk add --no-cache ca-certificates wget \
    && addgroup -S app && adduser -S app -G app

WORKDIR /app

COPY --from=build --chown=app:app /out/wallet-service ./wallet-service
COPY --chown=app:app migrations ./migrations

USER app

EXPOSE 8080

ENTRYPOINT ["./wallet-service"]