FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/wallet-server ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/wallet-server /app/wallet-server
USER app
EXPOSE 8080
ENTRYPOINT ["/app/wallet-server"]