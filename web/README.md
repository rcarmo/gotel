# web

Frontend and Bun server for the GoTel web UI.

## Local development

```bash
bun install --frozen-lockfile
bun run build:all
bun run serve
```

The server listens on `http://localhost:3000` and proxies API requests to `http://localhost:3200` by default.

## Environment

- `GOTEL_WEB_HOST` — listener address (default `127.0.0.1`; container `0.0.0.0`)
- `GOTEL_API_URL` — upstream collector/query API URL (default `http://localhost:3200`)
- `GOTEL_API_TIMEOUT_MS` — upstream timeout in milliseconds (default `5000`)
- `GOTEL_ALLOWED_ORIGINS` — optional comma-separated list of extra browser origins allowed for cross-origin access. If unset, the server stays same-origin only.

## Validation

```bash
bun test
bun x tsc --noEmit
bun run build:all
```
