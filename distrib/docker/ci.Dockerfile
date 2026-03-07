# Multi-stage Dockerfile for CI builds

# Build stage for Go binary
# Use BUILDPLATFORM to build on native architecture (fast)
FROM --platform=$BUILDPLATFORM golang:1.26-alpine3.22 AS go-builder

# Install build dependencies
RUN apk add --no-cache git make

# Build arguments
ARG VERSION=dev
ARG BUILDTIME
ARG REVISION
ARG BUILDER
ARG POLAR_ORG_ID=""

# Cross-compilation arguments from Docker BuildKit
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Set cross-compilation environment variables
ENV GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH}

# Handle ARM variants
RUN case "${TARGETARCH}" in \
      "arm") \
        case "${TARGETVARIANT}" in \
          "v6") export GOARM=6 ;; \
          "v7") export GOARM=7 ;; \
        esac \
      ;; \
    esac

# Build the binary with ldflags for target platform
RUN CGO_ENABLED=0 go build -trimpath -ldflags="\
    -s -w \
    -X github.com/autobrr/qui/internal/buildinfo.Version=${VERSION} \
    -X github.com/autobrr/qui/internal/buildinfo.Date=${BUILDTIME} \
    -X github.com/autobrr/qui/internal/buildinfo.Commit=${REVISION} \
    -X main.PolarOrgID=${POLAR_ORG_ID}" \
    -o qui ./cmd/qui

# Final stage
FROM alpine:latest AS runner

LABEL org.opencontainers.image.source="https://github.com/autobrr/qui"
LABEL org.opencontainers.image.licenses="GPL-2.0-or-later"
LABEL org.opencontainers.image.base.name="alpine:latest"

# Set environment variables for config paths
ENV HOME="/config" \
    XDG_CONFIG_HOME="/config" \
    XDG_DATA_HOME="/config"

# Install runtime dependencies
RUN apk --no-cache add ca-certificates curl tzdata bash

WORKDIR /config

# Declare volume for persistent data
VOLUME /config

# Copy binary from build stage
COPY --from=go-builder /app/qui /usr/local/bin/

EXPOSE 7476

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:7476/health || exit 1

ENTRYPOINT ["/usr/local/bin/qui"]
CMD ["serve"]
