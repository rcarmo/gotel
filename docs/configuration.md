# Configuration

## Collector Configuration (config.yaml)

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

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
    db_path: ${GOTEL_DB_PATH:-gotel.db}
    prefix: otel
    namespace: ""
    send_metrics: true
    store_traces: true
    retention: ${GOTEL_RETENTION:-168h}  # default 168h (~7 days)
    cleanup_interval: 1h
    query_port: 3200

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [sqlite]
```

## SQLite Exporter Options

| Option             | Type     | Default    | Description                                     |
| ------------------ | -------- | ---------- | ----------------------------------------------- |
| `db_path`          | string   | `gotel.db` | Path to SQLite database file                    |
| `prefix`           | string   | `otel`     | Root metric name prefix                         |
| `namespace`        | string   | `""`       | Additional namespace between prefix and service |
| `send_metrics`     | bool     | `true`     | Enable metric generation from traces            |
| `store_traces`     | bool     | `true`     | Store raw trace/span data for querying          |
| `tag_support`      | bool     | `false`    | Use Graphite 1.1+ tagged metric format          |
| `retention`        | duration | `168h`     | How long to keep data (default 168h)            |
| `cleanup_interval` | duration | `1h`       | How often to run cleanup                        |
| `query_port`       | int      | `3200`     | HTTP port for Tempo/Graphite query API          |

## Environment Variables

| Variable         | Description                                                                           |
| ---------------- | ------------------------------------------------------------------------------------- |
| `GOTEL_DB_PATH`  | Path to SQLite database file (default: `gotel.db`)                                    |
| `GOTEL_CONFIG`   | Path to config file. If missing, embedded defaults are used.                          |
| `GOTEL_RETENTION`| Overrides `retention` duration (e.g. `168h`).                                         |

When using Docker Compose, you can override settings:

```bash
GOTEL_DB_PATH=/data/traces.db GOTEL_RETENTION=1440h docker-compose up
```

## Metric Namespace

### Metric Path Structure

```plain
<prefix>.<namespace>.<service_name>.<operation_name>.<metric_type>
```

With the default configuration (`prefix: otel`, no namespace):

```plain
otel.<service>.<operation>.span_count
otel.<service>.<operation>.duration_ms
otel.<service>.<operation>.error_count  # Only emitted when errors > 0
```

### Using Namespaces

Namespaces help separate different environments or deployments:

```yaml
exporters:
  sqlite:
    prefix: otel
    namespace: production
    retention: ${GOTEL_RETENTION:-168h}
```

Results in:

```plain
otel.production.api_gateway.GET__users.span_count
otel.production.api_gateway.GET__users.duration_ms
```

### Example Metrics

For a service named `api-gateway` with an operation `GET /users`:

```plain
otel.api_gateway.GET__users.span_count 42 1704672000
otel.api_gateway.GET__users.duration_ms 125 1704672000
otel.api_gateway.GET__users.error_count 3 1704672000
```

### Character Sanitization

The exporter sanitizes metric names by replacing invalid characters:

| Character        | Replacement |
| ---------------- | ----------- |
| Space ` `        | `_`         |
| Slash `/`        | `_`         |
| Colon `:`        | `_`         |
| Parentheses `()` | `_`         |
| Brackets `[]{}`  | `_`         |
| Semicolon `;`    | `_`         |
| Equals `=`       | `_`         |

### Tagged Format (Graphite 1.1+)

When `tag_support: true`:

```plain
metric.name;service=api-gateway;operation=GET__users;status=ok value timestamp
```

## Storage Layout

Spans are stored as JSON with virtual generated columns extracted for indexing:

```sql
CREATE TABLE spans (
    id INTEGER PRIMARY KEY,
    data TEXT NOT NULL,
    created_at INTEGER,
    trace_id TEXT GENERATED ALWAYS AS (json_extract(data, '$.trace_id')) VIRTUAL,
    service_name TEXT GENERATED ALWAYS AS (json_extract(data, '$.service_name')) VIRTUAL,
    span_name TEXT GENERATED ALWAYS AS (json_extract(data, '$.span_name')) VIRTUAL,
    start_time_unix_nano INTEGER GENERATED ALWAYS AS (json_extract(data, '$.start_time_unix_nano')) VIRTUAL,
    status_code INTEGER GENERATED ALWAYS AS (json_extract(data, '$.status.code')) VIRTUAL
);

CREATE INDEX idx_spans_trace_id ON spans(trace_id);
CREATE INDEX idx_spans_service_name ON spans(service_name);
CREATE INDEX idx_spans_start_time ON spans(start_time_unix_nano);
```

## Retention and Cleanup

Data is automatically cleaned up based on the `retention` setting:

```yaml
exporters:
  sqlite:
    retention: ${GOTEL_RETENTION:-168h}  # Keep 168h of data by default
    cleanup_interval: 1h   # Run cleanup every hour
```

## Query API Endpoints

The SQLite exporter serves both Tempo-compatible and Graphite-compatible APIs on `query_port`:

| Endpoint | Protocol | Description |
|----------|----------|-------------|
| `/api/traces/{id}` | Tempo | Get trace by ID |
| `/api/search?service=X&operation=Y` | Tempo | Search traces |
| `/api/services` | Tempo | List available services |
| `/render?target=X` | Graphite | Render metric data |
| `/metrics/find?query=X` | Graphite | Find metric names |
| `/api/status` | Custom | Storage statistics |
| `/ready` | Custom | Health check |
