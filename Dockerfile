FROM golang:1.24-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# The SQLite and PostgreSQL drivers are both pure Go, so the binary is static
# and needs no runtime libraries.
RUN CGO_ENABLED=0 go build -o /out/walletd ./cmd/walletd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/walletd /walletd
EXPOSE 8080
ENTRYPOINT ["/walletd"]
