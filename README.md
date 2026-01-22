# gotel

![Gotel Logo](docs/icon-256.png)

A self-contained, single-binary OpenTelemetry Collector with built-in SQLite storage for traces and metrics, featuring a built-in web visualizer for exploring trace data, meant for small deployments and local development.

## Quick Start

```bash
# Using Docker Compose (includes Grafana)
docker compose up -d

# SQLite DB is created at the repo root:
#   ./gotel.db

# Or build from source
go build -o gotel .
./gotel
```

> The binary ships with an embedded default config (OTLP gRPC/HTTP → memory_limiter + batch → SQLite). Drop your own config at `config.yaml` or set `GOTEL_CONFIG`/`OTEL_CONFIG_FILE` to override.

## Endpoints

| Service    | Port | Description                          |
| ---------- | ---- | ------------------------------------ |
| OTLP gRPC  | 4317 | Trace ingestion (gRPC)               |
| OTLP HTTP  | 4318 | Trace ingestion (HTTP)               |
| Query API  | 3200 | Query API for trace data             |
| Web UI     | 3000 | Built-in trace visualizer            |

## Query API

The built-in query server provides endpoints for accessing trace data:

### Trace Query Endpoints

```bash
# Get trace by ID
curl http://localhost:3200/api/traces/{traceId}

# Search traces
curl http://localhost:3200/api/search?service=my-service

# List services
curl http://localhost:3200/api/services

# List all traces
curl http://localhost:3200/api/traces

# List spans
curl http://localhost:3200/api/spans

# List exceptions
curl http://localhost:3200/api/exceptions
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

- [Configuration](docs/configuration.md) - Exporter options and setup
- [Web UI](docs/web-ui.md) - Using the built-in trace visualizer
- [Sending Traces](docs/sending-traces.md) - Client examples (Go, Python, Node.js)
- [Development](docs/development.md) - Building and extending
- [Troubleshooting](docs/troubleshooting.md) - Common issues

## License

MIT
