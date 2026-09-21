# gotel

![Gotel Logo](docs/icon-256.png)

A small OpenTelemetry collector with SQLite-backed trace storage and a separate Bun-powered web UI for errors, slow operations, trace search, metrics and agent/LLM usage.

The native UI links metric buckets to complete traces and exports redacted investigation bundles for local, read-only inspection. It uses the existing Preact frontend, without Jaeger. See [native insights](docs/native-insights.md) for limits and telemetry semantics.

## Quick Start

### Docker Compose

```bash
docker compose up -d --build
```

This starts two services:

- `gotel` — collector + query API
- `web` — web UI proxy/static server

Persistent data is stored in the named Docker volume `gotel-data`. Published ports bind to loopback by default; set `GOTEL_BIND_HOST` to opt into remote access. The API has no built-in authentication—see [network access](docs/configuration.md#network-access) before exposing it.

Open:

- Web UI: http://localhost:3000
- Query API: http://localhost:3200

### From source

```bash
go build -o gotel .
./gotel
```

In a second shell, start the web UI:

```bash
make deps-frontend
make build-frontend
cd web && bun run serve
```

> The collector ships with an embedded default config (OTLP gRPC/HTTP → memory_limiter + batch → SQLite). Add `config.yaml` or set `GOTEL_CONFIG`/`OTEL_CONFIG_FILE` to override it.

## Ports

### Collector

| Service | Port | Description |
| --- | ---: | --- |
| OTLP gRPC | 4317 | Trace ingestion over gRPC |
| OTLP HTTP | 4318 | Trace ingestion over HTTP |
| Query API | 3200 | Trace and metrics query API |

### Web UI

| Service | Port | Description |
| --- | ---: | --- |
| Web UI | 3000 | Bun web server serving the frontend and proxying API requests |

## Query API

Useful endpoints exposed by the collector:

```bash
# List traces
curl http://localhost:3200/api/traces

# Fetch all stored spans for one trace
curl http://localhost:3200/api/traces/{traceId}/spans

# List recent spans
curl http://localhost:3200/api/spans

# List exceptions
curl http://localhost:3200/api/exceptions

# List services
curl http://localhost:3200/api/services

# Storage statistics
curl http://localhost:3200/api/status

# Readiness check
curl http://localhost:3200/ready
```

## Generated Metrics

Traces are converted to metrics stored in SQLite:

```text
otel.<service>.<operation>.span_count
otel.<service>.<operation>.duration_ms
otel.<service>.<operation>.error_count
```

`duration_ms` is a per-batch average, not a histogram or a percentile. Metric names sanitise service/operation names; original names remain in tags.

The native UI computes its own distributions from bounded raw-span queries through `/api/insights`; it does not average these batch averages.

## Validation

```bash
make deps-frontend check test-race test-frontend smoke
```

The smoke test starts isolated collector/web processes, ingests a 125-span trace, checks exact OTLP timestamps and resource identity, and verifies query ranges, native aggregates, agent usage, export redaction, frontend bundle validation, browser-origin policy and built assets.

## Documentation

- [Configuration](docs/configuration.md)
- [Web UI](docs/web-ui.md)
- [Sending Traces](docs/sending-traces.md)
- [Development](docs/development.md)
- [Troubleshooting](docs/troubleshooting.md)

## License

MIT
