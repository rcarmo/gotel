# Go build stage
FROM golang:1.21-alpine AS gotel-builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates build-base

# Copy go mod files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build binary (go-sqlite3 requires CGO)
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o gotel .

# Bun build stage
FROM oven/bun:1.1 AS web-builder

WORKDIR /app/web

# Copy package files
COPY web/package.json web/bun.lock ./

# Install dependencies
RUN bun install

# Copy web source
COPY web/ ./

# Build web assets
RUN bun build server.ts --outdir ./dist --target bun

# Runtime stage
FROM alpine:3.19

WORKDIR /app

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata sqlite

# Copy binaries from builders
COPY --from=gotel-builder /app/gotel .
COPY --from=web-builder /app/web/dist ./web

# Create data directory
RUN mkdir -p /data

# Expose ports
# 4317 - OTLP gRPC
# 4318 - OTLP HTTP
# 8888 - Metrics
# 3000 - Web UI
# 3200 - Query API
EXPOSE 4317 4318 8888 3000 3200

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8888/metrics || exit 1

# Run collector
ENTRYPOINT ["./gotel"]
