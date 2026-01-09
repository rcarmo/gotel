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
    retention: ${GOTEL_RETENTION:-168h} # default 168h (7 days)
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
| `retention`        | duration | `168h`     | How long to keep data (default 168h / 7 days)   |
| `cleanup_interval` | duration | `1h`       | How often to run cleanup                        |
| `query_port`       | int      | `3200`     | HTTP port for Tempo/Graphite query API          |

## Environment Variables

| Variable          | Description                                                  |
| ----------------- | ------------------------------------------------------------ |
| `GOTEL_DB_PATH`   | Path to SQLite database file (default: `gotel.db`)           |
| `GOTEL_CONFIG`    | Path to config file. If missing, embedded defaults are used. |
| `GOTEL_RETENTION` | Overrides `retention` duration (e.g. `168h`).                |

When using Docker Compose, you can override settings:

```bash
GOTEL_DB_PATH=/data/traces.db GOTEL_RETENTION=1440h docker-compose up
```

## Metric Namespace

Gotel automatically derives time-series metrics from ingested traces. These metrics are stored in the SQLite `metrics` table and exposed via the Graphite-compatible `/render` and `/metrics/find` endpoints.

### Metric Types

| Metric        | Description                                               |
| ------------- | --------------------------------------------------------- |
| `span_count`  | Number of spans observed for this service/operation       |
| `duration_ms` | Average duration in milliseconds                          |
| `error_count` | Number of spans with error status (only emitted when > 0) |

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

### Graphite Query Examples

Use these patterns with the `/render` and `/metrics/find` endpoints:

```bash
# List all metric paths under otel
curl "http://localhost:3200/metrics/find?query=otel.*"

# Get span counts for all operations in a service
curl "http://localhost:3200/render?target=otel.api_gateway.*.span_count&format=json"

# Sum all span counts across services
curl "http://localhost:3200/render?target=sumSeries(otel.*.*.span_count)&format=json"

# Average latency for a specific operation
curl "http://localhost:3200/render?target=otel.api_gateway.GET__users.duration_ms&format=json"

# All error counts (wildcard)
curl "http://localhost:3200/render?target=otel.*.*.error_count&format=json"

# With namespace (e.g., production environment)
curl "http://localhost:3200/render?target=otel.production.*.*.span_count&format=json"
```

In Grafana, use these as Graphite queries:

| Use Case                    | Query                                                                           |
| --------------------------- | ------------------------------------------------------------------------------- |
| Requests per service        | `otel.*.*.span_count`                                                           |
| Latency for one service     | `otel.my_service.*.duration_ms`                                                 |
| Error rate calculation      | `divideSeries(sumSeries(otel.*.*.error_count), sumSeries(otel.*.*.span_count))` |
| Top 5 operations by traffic | `highestCurrent(otel.*.*.span_count, 5)`                                        |
| Compare environments        | `group(otel.production.*.*.span_count, otel.staging.*.*.span_count)`            |

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

Spans are stored as JSON with full OpenTelemetry data including resource attributes, instrumentation scope, span links, and trace state. Virtual generated columns are extracted for indexing:

```sql
CREATE TABLE spans (
    id INTEGER PRIMARY KEY,
    data TEXT NOT NULL,
    created_at INTEGER,

    -- Core span fields
    trace_id TEXT GENERATED ALWAYS AS (json_extract(data, '$.trace_id')) VIRTUAL,
    span_id TEXT GENERATED ALWAYS AS (json_extract(data, '$.span_id')) VIRTUAL,
    parent_span_id TEXT GENERATED ALWAYS AS (json_extract(data, '$.parent_span_id')) VIRTUAL,
    service_name TEXT GENERATED ALWAYS AS (json_extract(data, '$.service_name')) VIRTUAL,
    span_name TEXT GENERATED ALWAYS AS (json_extract(data, '$.span_name')) VIRTUAL,
    start_time_unix_nano INTEGER GENERATED ALWAYS AS (json_extract(data, '$.start_time_unix_nano')) VIRTUAL,
    end_time_unix_nano INTEGER GENERATED ALWAYS AS (json_extract(data, '$.end_time_unix_nano')) VIRTUAL,
    duration_ns INTEGER GENERATED ALWAYS AS (...) VIRTUAL,
    status_code INTEGER GENERATED ALWAYS AS (json_extract(data, '$.status.code')) VIRTUAL,

    -- Resource attributes
    service_version TEXT GENERATED ALWAYS AS (json_extract(data, '$.resource."service.version"')) VIRTUAL,
    deployment_environment TEXT GENERATED ALWAYS AS (json_extract(data, '$.resource."deployment.environment"')) VIRTUAL,

    -- Instrumentation scope
    scope_name TEXT GENERATED ALWAYS AS (json_extract(data, '$.scope.name')) VIRTUAL
);

-- Indexes
CREATE INDEX idx_spans_trace_id ON spans(trace_id);
CREATE INDEX idx_spans_service_name ON spans(service_name);
CREATE INDEX idx_spans_span_name ON spans(span_name);
CREATE INDEX idx_spans_start_time ON spans(start_time_unix_nano);
CREATE INDEX idx_spans_status_code ON spans(status_code);
CREATE INDEX idx_spans_service_span ON spans(service_name, span_name);
CREATE INDEX idx_spans_created_at ON spans(created_at);
CREATE INDEX idx_spans_service_version ON spans(service_version);
CREATE INDEX idx_spans_deployment_env ON spans(deployment_environment);
CREATE INDEX idx_spans_scope_name ON spans(scope_name);
```

### Stored Span Fields

| Field                  | Description                                                             |
| ---------------------- | ----------------------------------------------------------------------- |
| `trace_id`             | 32-hex trace identifier                                                 |
| `span_id`              | 16-hex span identifier                                                  |
| `parent_span_id`       | Parent span ID (empty for root spans)                                   |
| `service_name`         | Extracted from `service.name` resource attribute                        |
| `span_name`            | Operation name                                                          |
| `kind`                 | Span kind (INTERNAL, SERVER, CLIENT, PRODUCER, CONSUMER)                |
| `start_time_unix_nano` | Start timestamp in nanoseconds                                          |
| `end_time_unix_nano`   | End timestamp in nanoseconds                                            |
| `status`               | Status code and message                                                 |
| `trace_state`          | W3C trace state (if present)                                            |
| `resource`             | All resource attributes (service.version, deployment.environment, etc.) |
| `scope`                | Instrumentation scope name and version                                  |
| `attributes`           | Span attributes                                                         |
| `links`                | Span links with trace_id, span_id, and attributes                       |
| `events`               | Span events with name, timestamp, and attributes                        |

## Retention and Cleanup

Data is automatically cleaned up based on the `retention` setting:

```yaml
exporters:
  sqlite:
    retention: ${GOTEL_RETENTION:-168h} # Keep 168h (7 days) of data by default
    cleanup_interval: 1h # Run cleanup every hour
```

## Query API Endpoints

The SQLite exporter serves both Tempo-compatible and Graphite-compatible APIs on `query_port`:

| Endpoint                            | Protocol | Description             |
| ----------------------------------- | -------- | ----------------------- |
| `/api/traces/{id}`                  | Tempo    | Get trace by ID         |
| `/api/search?service=X&operation=Y` | Tempo    | Search traces           |
| `/api/services`                     | Tempo    | List available services |
| `/render?target=X`                  | Graphite | Render metric data      |
| `/metrics/find?query=X`             | Graphite | Find metric names       |
| `/api/status`                       | Custom   | Storage statistics      |
| `/ready`                            | Custom   | Health check            |
