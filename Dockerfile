FROM golang:1.26.3-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server

FROM alpine:3.20
RUN adduser -D -H appuser
COPY --from=build /out/server /usr/local/bin/server
USER appuser
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
