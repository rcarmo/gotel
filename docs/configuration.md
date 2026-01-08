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
  graphite:
    endpoint: localhost:2003
    timeout: 10s
    prefix: otel
    namespace: traces
    send_metrics: true
    tag_support: false

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [graphite]
```

## Graphite Exporter Options

| Option         | Type     | Default          | Description                                     |
| -------------- | -------- | ---------------- | ----------------------------------------------- |
| `endpoint`     | string   | `localhost:2003` | Graphite Carbon server address (host:port)      |
| `timeout`      | duration | `10s`            | Connection and write timeout                    |
| `prefix`       | string   | `otel`           | Root metric name prefix                         |
| `namespace`    | string   | `""`             | Additional namespace between prefix and service |
| `send_metrics` | bool     | `true`           | Enable metric generation from traces            |
| `tag_support`  | bool     | `false`          | Use Graphite 1.1+ tagged metric format          |

## Environment Variables

| Variable           | Description                                                                           |
| ------------------ | ------------------------------------------------------------------------------------- |
| `OTEL_CONFIG_FILE` | Path to config file (default: `config.yaml`). If missing, embedded defaults are used. |

When using Docker Compose, you can override settings:

```bash
GRAPHITE_ENDPOINT=graphite:2003 docker-compose up
```

## Metric Namespace

### Metric Path Structure

```plain
<prefix>.<namespace>.<service_name>.<operation_name>.<metric_type>
```

With the default configuration (`prefix: otel`, `namespace: traces`):

```plain
otel.traces.<service>.<operation>.span_count
otel.traces.<service>.<operation>.duration_ms
otel.traces.<service>.<operation>.error_count  # Only emitted when errors > 0
```

### Example Metrics

For a service named `api-gateway` with an operation `GET /users`:

```plain
otel.traces.api_gateway.GET__users.span_count 42 1704672000
otel.traces.api_gateway.GET__users.duration_ms 125 1704672000
otel.traces.api_gateway.GET__users.error_count 3 1704672000
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
otel.traces.api_gateway.GET__users.span_count;service=api_gateway;span=GET__users 42 1704672000
```
