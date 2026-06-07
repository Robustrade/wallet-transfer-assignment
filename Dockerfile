# syntax=docker/dockerfile:1

# ---- Build stage ---------------------------------------------------------
# Pinned to Go 1.24 to match the CI toolchain.
FROM golang:1.24 AS build

WORKDIR /src

# Download dependencies first so this layer is cached unless go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download

# Build a static binary (CGO disabled) so it runs in a scratch/distroless image.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/server ./cmd/server

# ---- Runtime stage -------------------------------------------------------
# Distroless gives us a tiny, non-root image with CA certificates included.
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=build /out/server /app/server

# Document the default port; the actual bind address is configurable via HTTP_ADDR.
EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/app/server"]
