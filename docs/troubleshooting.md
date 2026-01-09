# Troubleshooting

## Common Issues

### Connection refused to Query API

```
Error: failed to connect to Gotel query API at localhost:3200
```

**Solutions:**
- Ensure the `gotel` service is running: `docker-compose ps gotel`
- Confirm `query_port` in `config.yaml` matches the exposed port
- Verify HTTP connectivity: `curl http://localhost:3200/ready`

### No metrics appearing in Grafana/Graphite datasource

**Checklist:**
- Verify traces are being received: check collector logs
- Ensure `service.name` attribute is set in your application
- Query the metrics endpoint: `curl "http://localhost:3200/render?target=otel.*.*.span_count&format=json"`
- Confirm your Grafana datasource URL points to port 3200

### High memory usage

**Solutions:**
- Adjust `memory_limiter` in config.yaml:
  ```yaml
  processors:
    memory_limiter:
      check_interval: 1s
      limit_mib: 256
      spike_limit_mib: 64
  ```
- Reduce `send_batch_size` in batch processor

### Traces not being exported

**Check:**
1. Collector is receiving traces (enable debug logging)
2. Query API endpoint (port 3200) matches datasource configuration
3. No network firewall blocking exposed ports (4317/4318/3200)

## Debug Mode

Enable debug logging in `config.yaml`:

```yaml
service:
  telemetry:
    logs:
      level: debug
```

## Viewing Collector Logs

```bash
# Docker Compose
docker-compose logs -f gotel

# Standalone
./gotel --config config.yaml 2>&1 | tee gotel.log
```

## Testing Connectivity

### Test OTLP gRPC

```bash
grpcurl -plaintext localhost:4317 list
```

### Test Query API

```bash
curl http://localhost:3200/api/status
```

### Check Graphite-compatible metrics

```bash
curl "http://localhost:3200/render?target=otel.traces.*.*.*&format=json"
```

### Verify collector is running

```bash
curl http://localhost:8888/metrics
```

## Performance Tuning

### For high-throughput environments

```yaml
processors:
  batch:
    timeout: 1s
    send_batch_size: 5000
    send_batch_max_size: 10000

exporters:
  graphite:
    timeout: 30s
```

### For low-latency requirements

```yaml
processors:
  batch:
    timeout: 100ms
    send_batch_size: 100
```
