# gotel

![Gotel Logo](docs/icon-256.png)

A (highly) experimental single-purpose OpenTelemetry Collector with a Graphite Exporter for lightweight trace-to-metric conversion on systems with limited resources, and ability to forward traces to Tempo.s

## Quick Start

```bash
# Using Docker Compose (includes Graphite + Grafana)
docker-compose up -d

# Or build from source
go build -o gotel .
./gotel --config config.yaml
```

> The binary ships with an embedded default config (OTLP gRPC/HTTP → memory_limiter + batch → Graphite). Drop your own config at config.yaml or set OTEL_CONFIG_FILE to override.

## Endpoints

If you use the `docker-compose.yaml` provided, the following services will be available:

| Service   | Port | Description              |
| --------- | ---- | ------------------------ |
| OTLP gRPC | 4317 | Trace ingestion (gRPC)   |
| OTLP HTTP | 4318 | Trace ingestion (HTTP)   |
| Tempo     | 3200 | Trace query (Tempo UI/API)|
| Grafana   | 3000 | Dashboards (admin/admin) |
| Graphite  | 8080 | Web UI                   |

## Generated Metrics

Traces are converted to Graphite metrics:

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
