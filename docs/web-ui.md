# Web UI

The GoTel web UI is a separate Bun service that serves the frontend on port `3000` and proxies browser API requests to the collector query API on port `3200`.

## Start it

### With Docker Compose

```bash
docker compose up -d --build
```

Open http://localhost:3000.

### From source

```bash
make deps-frontend
make build-frontend
cd web && bun run serve
```

## Views and workflow

- **Overview** opens with error groups, previous-window comparisons, slow operations and service last-seen times.
- **Trace Search** filters by service, exact operation, time, error status, duration and an exact span attribute. Selecting a trace opens all its retained spans in **Timeline**.
- **Metrics** computes counts, duration sums, percentiles and distributions from bounded raw-span queries. Select a time bucket to search its traces.
- **Agents** shows model/provider latency, token usage, supplied USD costs, explicit retries and tool failures when instrumentation supplies the attributes.
- **Export selected** saves up to 20 complete traces and their aggregate context as allowlisted investigation JSON. The timeline also supports exporting its selected trace.
- **Import bundle** opens that evidence locally, read-only, without uploading it or querying the live backend. **Exit import** restores live queries.

The shared filters start at 24 hours. Relative presets advance on refresh; custom windows remain fixed. Native aggregate queries reject more than 20,000 matching spans rather than showing partial totals. Narrow the window or service filter when prompted.

Names and labels can remain sensitive after export; review a bundle before sharing it. The [native insights contract](native-insights.md) defines filters, sampling caveats, attribute mappings, export redaction and limits.

## API calls used by the UI

| Endpoint | Method | Purpose |
| --- | --- | --- |
| `/api/insights` | GET | Landing page, raw-span metrics and agent aggregates |
| `/api/explore` | GET | Filtered complete trace summaries |
| `/api/investigation` | GET | Redacted selected traces and context bundle |
| `/api/traces/{id}/spans` | GET | All retained spans for the selected trace |
| `/api/services` | GET | Service discovery |

The frontend proxy preserves query parameters and response bodies. The older `/api/traces`, `/api/spans` and `/api/exceptions` routes remain available.

## CORS and browser access

The web server is same-origin by default. Cross-origin browser access is disabled unless you set `GOTEL_ALLOWED_ORIGINS`.

Example:

```bash
GOTEL_ALLOWED_ORIGINS=https://ops.example.com,https://grafana.example.com bun run serve
```

If the UI and API are served from the same origin through a reverse proxy, you do not need extra CORS settings.

## Troubleshooting

### The page loads but charts or timelines are empty

Check the proxied endpoints directly:

```bash
curl http://localhost:3000/api/insights
curl http://localhost:3000/api/explore
curl 'http://localhost:3000/api/traces/<trace-id>/spans'
```

### Static assets return 404

Rebuild the frontend assets:

```bash
make build-frontend
```

Then restart the web server.

### The UI cannot reach the collector

Make sure the collector query API is reachable:

```bash
curl http://localhost:3200/ready
curl http://localhost:3200/api/status
```
