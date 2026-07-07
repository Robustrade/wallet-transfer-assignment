FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/wallet-api ./cmd/api

FROM alpine:3.21

RUN addgroup -S app && adduser -S app -G app
COPY --from=build /out/wallet-api /usr/local/bin/wallet-api
USER app
EXPOSE 8080
ENTRYPOINT ["wallet-api"]
