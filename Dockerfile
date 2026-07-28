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
