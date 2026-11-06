ARG GO_VERSION=1.24

# ---- deps: download modules once, cached as a layer -------------------------
FROM golang:${GO_VERSION}-bookworm AS deps
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

# ---- test: full toolchain with gcc (the race detector needs cgo) ------------
FROM deps AS test
COPY . .
CMD ["go", "test", "./...", "-race", "-cover", "-count=1"]

# ---- build: static binary ----------------------------------------------------
FROM deps AS build
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/wallet ./cmd/wallet

# ---- runtime: minimal, non-root, no shell (default target) ------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/wallet /usr/local/bin/wallet
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/wallet"]
CMD ["serve"]
