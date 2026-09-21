# Configuration

## Collector configuration (`config.yaml`)

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: ${env:GOTEL_OTLP_HOST:-127.0.0.1}:4317
      http:
        endpoint: ${env:GOTEL_OTLP_HOST:-127.0.0.1}:4318

processors:
  batch:
    timeout: 5s
    send_batch_size: 1000

  memory_limiter:
    check_interval: 1s
    limit_mib: 512
    spike_limit_mib: 128

exporters:
  sqlite:
    db_path: gotel.db
    prefix: otel
    namespace: ""
    send_metrics: true
    store_traces: true
    retention: 168h
    cleanup_interval: 1h
    query_port: 3200
    query_host: 127.0.0.1
    allowed_origins: []

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [sqlite]
```

## SQLite exporter options

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `db_path` | string | `gotel.db` | Path to the SQLite database |
| `prefix` | string | `otel` | Root metric name prefix |
| `namespace` | string | `""` | Optional namespace between prefix and service |
| `send_metrics` | bool | `true` | Enable derived metrics from traces |
| `store_traces` | bool | `true` | Persist raw span/trace data |
| `retention` | duration | `168h` | Data retention period |
| `cleanup_interval` | duration | `1h` | Cleanup cadence |
| `query_port` | int | `3200` | Query API port, from 0 to 65535 (`0` disables it) |
| `query_host` | string | `127.0.0.1` | Query listener IP or `localhost` |
| `allowed_origins` | list | `[]` | Exact additional browser origins (no wildcard) |

Negative retention and cleanup durations are rejected. Zero uses the default. A query-port bind failure fails Collector startup.

## Environment variables

| Variable | Description |
| --- | --- |
| `GOTEL_DB_PATH` | Path to SQLite database file |
| `GOTEL_CONFIG` | Config file path |
| `GOTEL_RETENTION` | Retention override, e.g. `72h` |
| `GOTEL_QUERY_HOST` | Override query listener address |
| `GOTEL_ALLOWED_ORIGINS` | Comma-separated exact browser origins, e.g. `https://traces.example.com` |
| `GOTEL_OTLP_HOST` | OTLP bind host used by embedded/example config; default `127.0.0.1` |
| `OTEL_CONFIG_FILE` | Config path when `GOTEL_CONFIG` is unset |

With Docker Compose:

```bash
GOTEL_RETENTION=1440h docker compose up -d
```

## Query API endpoints

| Endpoint | Description |
| --- | --- |
| `/api/traces` | List trace summaries |
| `/api/traces/{id}` | Get a trace by ID |
| `/api/traces/{id}/spans` | Return all raw stored spans for one trace |
| `/api/spans` | List recent spans |
| `/api/exceptions` | List exception records |
| `/api/services` | List service names |
| `/api/status` | Storage statistics |
| `/ready` | Health check |

The `/api/traces/{id}/spans` response is intended for the web timeline view and returns the same raw span object shape used by `/api/spans`, independently of the recent-span limit. Missing traces return HTTP 404.

## Graphite query bounds

`/render` accepts `from` and `until` as Unix seconds, RFC3339 timestamps, `now`, or negative intervals such as `-30min`, `-1h` and `-7d`. It defaults to the last 24 hours. GET query strings and URL-encoded POST forms use the same range handling.

There are at most 32 targets per request and 10,000 datapoints per target. Larger results return HTTP 422 instead of silently truncating. Narrow the time range or target. Rendering supports JSON, metric patterns, `aliasByNode`, `aliasSub`, and `aliasSub(aliasByNode(...), ...)`; unsupported expressions return HTTP 400. It does not implement Graphite's full expression language or downsampling.

`/metrics/find` queries distinct metric names, not a sample of their datapoints. More than 10,000 matching names returns HTTP 422; narrow the pattern.

## Network access

Native collector/query and web listeners default to loopback. Containers listen on their internal interfaces, but Compose publishes ports on `127.0.0.1` by default. Set `GOTEL_BIND_HOST` for Compose if remote access is required.

Cross-origin browser access is denied unless the exact origin is configured. Server-to-server clients without an `Origin` header do not require a CORS exception. For HTTPS reverse proxies, allow the external origin explicitly if the backend sees an HTTP request.

CORS is not authentication. Before exposing telemetry beyond a trusted machine/network, use an authenticated TLS reverse proxy or equivalent access controls. OTLP data, exception stacks and attributes can contain sensitive information.

## Metric namespace

Derived metrics use this pattern:

```text
<prefix>.<namespace>.<service_name>.<operation_name>.<metric_type>
```

With the defaults:

```text
otel.<service>.<operation>.span_count
otel.<service>.<operation>.duration_ms
otel.<service>.<operation>.error_count
```
