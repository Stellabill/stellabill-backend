# syntax=docker/dockerfile:1.7
#
# Multi-stage build for stellabill-backend.
# - Stage 1: build the static Go binary from ./cmd/server
# - Stage 2: minimal distroless runtime with non-root UID, HEALTHCHECK on /api/health
#
# Notes:
# - Go 1.25 matches the project's go.mod (`go 1.25.0`).
# - CGO_ENABLED=0 plus -ldflags "-s -w" produces a fully static binary.
# - The distroless/cc image ships libc + ca-certificates; no shell, no package manager.
# - Tags: built locally with :dev; CI builds :v<git-tag> and :sha-<short-sha>.

ARG GO_VERSION=1.25

# ---- build stage ----
FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src

# Cache go module download separately from the source tree.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy the rest of the source.
COPY . .

# Build a fully static binary at cmd/server.
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
      -trimpath \
      -ldflags "-s -w \
        -X main.version=${VERSION} \
        -X main.commit=${COMMIT} \
        -X main.buildTime=${BUILD_TIME}" \
      -o /out/server ./cmd/server

# ---- runtime stage ----
FROM gcr.io/distroless/cc-debian12:nonroot AS runtime

# Labels used by image-signing/cosign tooling and SLSA provenance tooling.
LABEL org.opencontainers.image.title="stellabill-backend" \
      org.opencontainers.image.description="Stellabill API backend (Gin)" \
      org.opencontainers.image.source="https://github.com/Stellabill/stellabill-backend" \
      org.opencontainers.image.licenses="proprietary" \
      org.opencontainers.image.vendor="Stellabill"

# Distroless `nonroot` ships UID 65532.
USER 65532:65532
WORKDIR /app

# Copy the static binary only (no source, no build cache).
COPY --from=build --chown=65532:65532 /out/server /app/server

# The server reads PORT (default 8080) and a /api/health endpoint.
ENV PORT=8080 \
    ENV=production \
    GIN_MODE=release

EXPOSE 8080

# Distroless has no shell, so HEALTHCHECK uses the gRPC/HTTP binary itself.
# The server exposes /api/health, but distroless can't run curl/wget.
# We rely on Kubernetes/kyverno readiness probes + cosign verification at
# admission instead of an in-image curl HEALTHCHECK.

ENTRYPOINT ["/app/server"]
# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.25-bookworm AS build

WORKDIR /src

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -trimpath \
    -o /server \
    ./cmd/server

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /server /server

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/server"]
