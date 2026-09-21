# Development Guide

## Project structure

```text
gotel/
├── main.go
├── Dockerfile
├── Dockerfile.web
├── Makefile
├── README.md
├── docs/
├── exporter/
├── storage/
└── web/
    ├── components/
    ├── index.ts
    ├── server.ts
    └── package.json
```

## Prerequisites

- Go 1.21+
- Bun
- Docker (optional)

## Collector workflow

```bash
make deps
make build
./gotel
```

## Frontend workflow

```bash
make deps-frontend
make typecheck
make build-frontend
cd web && bun test
cd web && bun run serve
```

The collector query API defaults to `http://localhost:3200`. The web UI defaults to `http://localhost:3000`.

## Docker Compose

```bash
docker compose up -d --build
```

Compose starts:

- `gotel` on ports `4317`, `4318`, `3200`
- `web` on port `3000`

## Architecture

```text
┌──────────────┐      ┌──────────────────────────────┐      ┌─────────────────┐
│ Your app     │ ───▶ │ gotel collector + query API │ ◀─── │ web UI proxy    │
│ OTLP client  │      │ SQLite-backed trace store   │      │ static frontend  │
└──────────────┘      └──────────────────────────────┘      └─────────────────┘
```

Notes:

- The collector stores traces and derived metrics in SQLite.
- The web UI is not embedded in the collector binary; it is a separate Bun process/container.
- The timeline view loads a selected trace from `/api/traces/{id}/spans`.
- Native insights use bounded SQLite projections and raw-span distributions; see [native insights](native-insights.md).

## Testing

For frontend-only work, run:

```bash
make deps-frontend
make check test-race typecheck test-frontend smoke
```

The optional real-browser smoke test requires Playwright and its Chromium build. It exercises the real API and download path, metric drilldown, local import and stale-response isolation:

```bash
PLAYWRIGHT_BROWSERS_PATH=/path/to/browsers \
GOTEL_PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs \
GOTEL_BROWSER_SMOKE=1 make smoke
```

Set `GOTEL_SCREENSHOT` to save a landing-page screenshot. Without `GOTEL_BROWSER_SMOKE`, the smoke test does not require Playwright. Test databases and servers are isolated and removed after the run.
