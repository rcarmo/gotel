# Troubleshooting

## Query API is unavailable

Example error:

```text
Failed to reach upstream API
```

Checks:

- Confirm the collector is running.
- Verify the query API on port `3200`.
- If using Compose, inspect the logs with `docker compose logs gotel web`.

```bash
curl http://localhost:3200/ready
curl http://localhost:3200/api/status
```

## Web UI loads but shows no traces

Checklist:

- Verify traces are reaching the collector.
- Ensure `service.name` is present in your telemetry.
- Query the collector directly:

```bash
curl http://localhost:3200/api/traces
curl http://localhost:3200/api/spans
```

- Query the web proxy directly:

```bash
curl http://localhost:3000/api/traces
curl http://localhost:3000/api/spans
```

## Selected trace timeline is incomplete or empty

The timeline view depends on the selected-trace endpoint:

```bash
curl http://localhost:3200/api/traces/<trace-id>/spans
curl http://localhost:3000/api/traces/<trace-id>/spans
```

If the collector returns an empty array, the UI will show an empty-state message rather than synthetic rows.

## Static assets return 404

Rebuild the frontend assets and restart the web server:

```bash
make build-frontend
cd web && bun run serve
```

## Cross-origin browser requests fail

The web UI is same-origin by default. Only set `GOTEL_ALLOWED_ORIGINS` when you intentionally expose the web server to a different browser origin.

```bash
GOTEL_ALLOWED_ORIGINS=https://ops.example.com bun run serve
```

## Useful commands

```bash
# Collector logs
docker compose logs -f gotel

# Web UI logs
docker compose logs -f web

# Local collector run
./gotel 2>&1 | tee gotel.log
```
