# gotel

![Gotel Logo](docs/icon-256.png)

A self-contained, single-binary OpenTelemetry Collector with built-in SQLite storage for traces and metrics. Uses WAL mode and JSON virtual columns with indexes for efficient querying. No external dependencies required.

## Quick Start

```bash
# Using Docker Compose (includes Grafana)
docker-compose up -d

# Or build from source
go build -o gotel .
./gotel --config config.yaml
```

> The binary ships with an embedded default config (OTLP gRPC/HTTP → memory_limiter + batch → SQLite). Drop your own config at config.yaml or set OTEL_CONFIG_FILE to override.

## Endpoints

| Service    | Port | Description                          |
| ---------- | ---- | ------------------------------------ |
| OTLP gRPC  | 4317 | Trace ingestion (gRPC)               |
| OTLP HTTP  | 4318 | Trace ingestion (HTTP)               |
| Query API  | 3200 | Tempo/Graphite-compatible query API  |
| Grafana    | 3000 | Dashboards (admin/admin)             |

## Query API

The built-in query server provides Tempo and Graphite compatible endpoints:

### Tempo-compatible

```bash
# Get trace by ID
curl http://localhost:3200/api/traces/{traceId}

# Search traces
curl http://localhost:3200/api/search?service=my-service

# List services
curl http://localhost:3200/api/services
```

### Graphite-compatible

```bash
# Render metrics
curl "http://localhost:3200/render?target=otel.traces.*.*.span_count&format=json"

# Find metrics
curl "http://localhost:3200/metrics/find?query=otel.traces.*"
```

### Status

```bash
# Storage statistics
curl http://localhost:3200/api/status

# Readiness check
curl http://localhost:3200/ready
```

## Generated Metrics

Traces are converted to metrics stored in SQLite:

```plain
otel.traces.<service>.<operation>.span_count
otel.traces.<service>.<operation>.duration_ms
otel.traces.<service>.<operation>.error_count
```

## Documentation

- [Configuration](docs/configuration.md) - Exporter options and metric namespace
- [Grafana Integration](docs/grafana.md) - Dashboard and useful queries
- [Sending Traces](docs/sending-traces.md) - Client examples (Go, Python, Node.js)
- [Development](docs/development.md) - Building and extending
- [Troubleshooting](docs/troubleshooting.md) - Common issues

## License

MIT
