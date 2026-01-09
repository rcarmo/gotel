# Grafana Integration

Gotel exposes both **Tempo-compatible** and **Graphite-compatible** endpoints on port 3200, allowing you to visualize traces and metrics in Grafana.

## Quick Start

```bash
docker-compose up -d
```

Open Grafana at http://localhost:3000 (default credentials: admin/admin).

---

## Data Sources

### Tempo Data Source (Trace Visualization)

The Tempo datasource enables trace exploration, span timelines, and distributed tracing views.

| Setting    | Value                                                           |
| ---------- | --------------------------------------------------------------- |
| **Name**   | Gotel                                                           |
| **Type**   | Tempo                                                           |
| **URL**    | `http://gotel:3200` (Docker) or `http://localhost:3200` (local) |
| **Access** | Server (default)                                                |

**Provisioning file** (`grafana/provisioning/datasources/gotel.yaml`):

```yaml
apiVersion: 1

datasources:
  - name: Gotel
    type: tempo
    url: http://gotel:3200
    access: proxy
    isDefault: true
    jsonData:
      httpMethod: POST
      nodeGraph:
        enabled: true
```

### Graphite Data Source (Metrics)

For time-series metrics derived from traces (span counts, durations, error rates).

| Setting     | Value               |
| ----------- | ------------------- |
| **Name**    | Gotel Metrics       |
| **Type**    | Graphite            |
| **URL**     | `http://gotel:3200` |
| **Version** | 1.1.x               |

**Provisioning file:**

```yaml
apiVersion: 1

datasources:
  - name: Gotel Metrics
    type: graphite
    url: http://gotel:3200
    access: proxy
    jsonData:
      graphiteVersion: "1.1"
```

---

## Exploring Traces

### Browse Traces

1. Open **Explore** (compass icon in sidebar)
2. Select **Gotel** datasource
3. Choose **Search** tab
4. Filter by:
   - **Service**: Select from dropdown
   - **Operation**: Filter by span name
   - **Duration**: Min/max latency
   - **Status**: OK or Error

### View Trace Timeline

1. Click on any trace in the search results
2. The trace view shows:
   - **Waterfall diagram**: Span hierarchy with timing
   - **Span details**: Attributes, events, status
   - **Parent-child relationships**: Visual span nesting

### Search by Trace ID

1. Select **TraceID** tab
2. Enter the full trace ID (32 hex characters)
3. Click **Run query**

---

## Metric Namespace Structure

Metrics are organized hierarchically:

```
<prefix>.<namespace>.<service_name>.<operation_name>.<metric_type>
```

### Default Configuration

With `prefix: otel` and no namespace:

```
otel.<service>.<operation>.span_count
otel.<service>.<operation>.duration_ms
otel.<service>.<operation>.error_count
```

### Using Namespaces for Environments

Set `namespace` in config to separate environments:

```yaml
exporters:
  sqlite:
    db_path: /data/gotel.db
    prefix: otel
    namespace: ${ENVIRONMENT:-default} # Set via env var
    send_metrics: true
    store_traces: true
```

**Results in:**

```
otel.production.api_gateway.GET__users.span_count
otel.staging.api_gateway.GET__users.span_count
```

---

## Pre-built Dashboard

Import the included dashboard from `grafana/dashboards/traces-overview.json`:

### Dashboard Panels

**Trace Explorer Row:**
1. **Recent Traces** - Table of recent traces with service, operation, duration
2. **Trace Timeline (Gantt View)** - Waterfall/Gantt visualization of selected trace spans

**Metrics Overview Row:**
3. **Span Count by Operation** - Time series of request throughput
4. **Latency by Operation** - Average response times (ms)
5. **Errors by Operation** - Stacked bar chart of errors
6. **Overall Error Rate** - Stat panel with color thresholds
7. **Traffic Distribution** - Pie chart of traffic by operation
8. **Operations Summary** - Table with aggregated metrics

### Using the Trace Explorer

1. Select a trace from the **Recent Traces** table
2. Copy the Trace ID
3. Enter it in the **Trace ID** variable at the top
4. The **Trace Timeline** panel shows a Gantt-style waterfall view

### Importing the Dashboard

**Option 1: Auto-provisioning (Docker Compose)**

Dashboard is automatically available when using `docker-compose up`.

**Option 2: Manual Import**

1. Open Grafana (http://localhost:3000)
2. Go to **Dashboards → Import**
3. Upload `grafana/dashboards/traces-overview.json`
4. Select your Gotel Metrics datasource

---

## Useful Queries

### Graphite Metric Queries

```graphite
# Total requests per service
sumSeries(otel.*.*.span_count)

# Average latency across all operations
averageSeries(otel.my_service.*.duration_ms)

# Error rate as percentage
scale(divideSeries(
  sumSeries(otel.*.*.error_count),
  sumSeries(otel.*.*.span_count)
), 100)

# Top 5 operations by request count
highestCurrent(otel.*.*.span_count, 5)

# Operations with errors only
exclude(otel.*.*.error_count, "0")

# Specific operation latency
otel.api_gateway.GET__api_users.duration_ms

# Compare environments (with namespace)
group(
  alias(otel.production.api.*.span_count, "Production"),
  alias(otel.staging.api.*.span_count, "Staging")
)
```

### TraceQL Queries (Tempo-style)

Note: Gotel implements a subset of Tempo's search API.

```
# Search by service
service=api-gateway

# Search by operation
service=api-gateway&operation=GET /users
```

---

## Creating Trace Panels

### Trace Search Panel

1. Add new panel
2. Select **Gotel** (Tempo) datasource
3. Query type: **Search**
4. Configure filters (service, operation)
5. Visualization: **Traces**

### Metrics Panel with Trace Links

1. Add panel with time series visualization
2. Use Gotel Metrics (Graphite) datasource
3. Configure data links to jump to traces

---

## Alerting

### High Error Rate Alert

```yaml
# In Grafana UI: Alerting → Alert Rules → New
name: High Error Rate
query: scale(divideSeries(sumSeries(otel.*.*.error_count), sumSeries(otel.*.*.span_count)), 100)
condition: WHEN avg() OF query IS ABOVE 5
for: 5m
```

### Slow Response Time Alert

```yaml
name: Slow Response Time
query: maxSeries(otel.*.*.duration_ms)
condition: WHEN avg() OF query IS ABOVE 500
for: 5m
```

---

## API Endpoints Reference

| Endpoint           | Protocol | Description        |
| ------------------ | -------- | ------------------ |
| `/api/traces/{id}` | Tempo    | Get trace by ID    |
| `/api/search`      | Tempo    | Search traces      |
| `/api/services`    | Tempo    | List services      |
| `/render`          | Graphite | Render metrics     |
| `/metrics/find`    | Graphite | Find metric paths  |
| `/api/status`      | Custom   | Storage statistics |
| `/ready`           | Custom   | Health check       |

---

## Troubleshooting

### No traces appearing

1. Check gotel logs: `docker-compose logs gotel`
2. Verify OTLP endpoint: `curl http://localhost:4318/v1/traces`
3. Check storage stats: `curl http://localhost:3200/api/status`

### Metrics not updating

1. Ensure `send_metrics: true` in config
2. Check metric retention settings
3. Verify Graphite datasource URL points to port 3200

### Slow queries

1. Check database size: `ls -lh /data/gotel.db`
2. Reduce retention period in config
3. Virtual columns are indexed by default for fast queries
