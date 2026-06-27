# syntax=docker/dockerfile:1

# ---- Build stage -----------------------------------------------------------
# Pin the toolchain to match go.mod (go 1.24 / toolchain go1.24.13). CGO is
# disabled so the resulting binary is fully static and runs on distroless.
FROM golang:1.24 AS build

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOTOOLCHAIN=go1.24.13

WORKDIR /src

# Cache module downloads independently of source changes.
COPY go.mod go.sum ./
RUN go mod download

# Build the static binary.
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/walletd ./cmd/walletd

# ---- Runtime stage ---------------------------------------------------------
# Distroless static: no shell, no package manager, minimal attack surface. The
# :nonroot tag runs as an unprivileged user (uid 65532).
FROM gcr.io/distroless/static:nonroot

# Document the default listen port (HTTP_ADDR=:8080).
EXPOSE 8080

USER nonroot:nonroot

COPY --from=build /out/walletd /walletd

ENTRYPOINT ["/walletd"]
