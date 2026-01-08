# Grafana Integration

## Pre-built Dashboard

The project includes a pre-built Grafana dashboard at `grafana/dashboards/traces-overview.json`.

### Dashboard Panels

1. **Span Count by Operation** - Time series of request throughput
2. **Latency by Operation** - Average response times (ms)
3. **Errors by Operation** - Stacked bar chart of errors
4. **Overall Error Rate** - Stat panel with color thresholds
5. **Traffic Distribution** - Pie chart of traffic by operation
6. **Operations Summary** - Table with aggregated metrics

## Importing the Dashboard

### Option 1: Auto-provisioning (Docker Compose)

The dashboard is automatically provisioned when using `docker-compose up`.

### Option 2: Manual Import

1. Open Grafana (http://localhost:3000)
2. Go to Dashboards → Import
3. Upload `grafana/dashboards/traces-overview.json`
4. Select your Graphite datasource

## Useful Graphite Queries

```graphite
# Total requests per service
sumSeries(otel.traces.my_service.*.span_count)

# Average latency across all operations
averageSeries(otel.traces.my_service.*.duration_ms)

# Max latency (approximates P99)
maxSeries(otel.traces.my_service.*.duration_ms)

# Error rate as percentage
scale(divideSeries(
  sumSeries(otel.traces.*.*.error_count),
  sumSeries(otel.traces.*.*.span_count)
), 100)

# Top 5 operations by request count
highestCurrent(otel.traces.my_service.*.span_count, 5)

# Operations with errors
exclude(otel.traces.my_service.*.error_count, "0")

# Specific operation latency over time
otel.traces.my_service.GET__api_users.duration_ms

# Compare services
group(
  alias(otel.traces.service_a.*.span_count, "Service A"),
  alias(otel.traces.service_b.*.span_count, "Service B")
)
```

## Creating Alerts

Example Grafana alert for high error rate:

```yaml
# In Grafana UI: Alerting → Alert Rules → New
Query: scale(divideSeries(sumSeries(otel.traces.*.*.error_count), sumSeries(otel.traces.*.*.span_count)), 100)
Condition: WHEN avg() OF query IS ABOVE 5
For: 5m
```
