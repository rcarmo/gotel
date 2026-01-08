# Troubleshooting

## Common Issues

### Connection refused to Graphite

```
Error: failed to connect to Graphite at localhost:2003
```

**Solutions:**
- Ensure Graphite is running: `docker-compose ps graphite`
- Check the endpoint configuration in `config.yaml`
- Verify network connectivity: `nc -zv localhost 2003`

### No metrics appearing in Graphite

**Checklist:**
- Verify traces are being received: check collector logs
- Ensure `service.name` attribute is set in your application
- Check Graphite web UI for metrics under `otel.traces.*`
- Verify the exporter is enabled in the pipeline

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
2. Graphite endpoint is correct
3. No network firewall blocking port 2003

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

### Test Graphite connection

```bash
echo "test.metric 42 $(date +%s)" | nc localhost 2003
```

### Check Graphite metrics

```bash
curl "http://localhost/render?target=otel.traces.*.*.*&format=json"
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
