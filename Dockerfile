# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o gotel .

# Runtime stage
FROM alpine:3.19

WORKDIR /app

# Install CA certificates for HTTPS
RUN apk --no-cache add ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /app/gotel .
COPY --from=builder /app/config.yaml .

# Expose ports
# 4317 - OTLP gRPC
# 4318 - OTLP HTTP
# 8888 - Metrics
EXPOSE 4317 4318 8888

# Run the collector
ENTRYPOINT ["./gotel"]
CMD ["--config", "config.yaml"]
