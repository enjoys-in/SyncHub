# ── Build Stage ─────────────────────────────────────────────
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath -o /bin/synchub ./cmd/server

# ── Runtime Stage (hardened Alpine) ────────────────────────
FROM alpine:3.20 AS runtime

# Install ca-certs for HTTPS webhooks and tzdata for time zones
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 1000 -S synchub \
    && adduser -u 1000 -S synchub -G synchub -h /app -s /sbin/nologin

# Harden the root filesystem
RUN rm -rf /var/cache/apk/* /tmp/* /var/tmp/* \
    && chmod 700 /root \
    && find / -type f -perm /6000 -exec chmod ug-s {} \; 2>/dev/null || true

WORKDIR /app

# Create data directory owned by non-root user
RUN mkdir -p /app/data && chown -R synchub:synchub /app

# Copy binary from builder
COPY --from=builder /bin/synchub /app/synchub

# Copy default data (will be overridden by volume mount)
COPY --chown=synchub:synchub data/ /app/data/

# Drop to non-root user
USER synchub:synchub

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/synchub"]
