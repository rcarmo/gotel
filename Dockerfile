# Go build stage
FROM docker.io/library/golang:1.24-alpine AS gotel-builder

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

# Runtime stage
FROM docker.io/library/alpine:3.19

WORKDIR /app

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata sqlite

# The web UI is built and served by Dockerfile.web.
COPY --from=gotel-builder /app/gotel .
ENV GOTEL_DB_PATH=/data/gotel.db \
    GOTEL_QUERY_HOST=0.0.0.0 \
    GOTEL_OTLP_HOST=0.0.0.0

# Create data directory
RUN mkdir -p /data

# Expose ports
# 4317 - OTLP gRPC
# 4318 - OTLP HTTP
# 3200 - Query API
EXPOSE 4317 4318 3200

# Health check against the query API readiness endpoint
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:3200/ready || exit 1

# Run collector
ENTRYPOINT ["./gotel"]
