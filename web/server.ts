import { file, serve } from 'bun';
import { normalize, resolve, sep } from 'path';

export type FetchFn = (input: string | URL | Request, init?: RequestInit) => Promise<Response>;

export interface ServerConfig {
  allowedOrigins: string[];
  upstreamUrl: string;
  upstreamTimeoutMs: number;
  distDir: string;
  nodeModulesDir: string;
  fetchFn: FetchFn;
}

export function createServerConfig(env: Record<string, string | undefined> = process.env): ServerConfig {
  const allowedOrigins = (env.GOTEL_ALLOWED_ORIGINS || '')
    .split(',')
    .map((origin) => origin.trim())
    .filter(Boolean);

  const parsedTimeoutMs = Number.parseInt(env.GOTEL_API_TIMEOUT_MS || '5000', 10);

  return {
    allowedOrigins,
    upstreamUrl: env.GOTEL_API_URL || 'http://localhost:3200',
    upstreamTimeoutMs: Number.isNaN(parsedTimeoutMs) || parsedTimeoutMs <= 0 ? 5000 : parsedTimeoutMs,
    distDir: resolve(import.meta.dir, 'dist'),
    nodeModulesDir: resolve(import.meta.dir, 'node_modules'),
    fetchFn: fetch
  };
}

function isSameOrigin(requestUrl: URL, requestOrigin: string): boolean {
  return requestOrigin === requestUrl.origin;
}

function buildCorsHeaders(requestUrl: URL, requestOrigin: string, allowedOrigins: string[]) {
  const headers = new Headers({
    'Access-Control-Allow-Methods': 'GET, OPTIONS',
    'Access-Control-Allow-Headers': 'Content-Type, Authorization',
    'Vary': 'Origin'
  });

  if (!requestOrigin || isSameOrigin(requestUrl, requestOrigin)) {
    return { headers, forbidden: false };
  }

  if (allowedOrigins.includes(requestOrigin)) {
    headers.set('Access-Control-Allow-Origin', requestOrigin);
    return { headers, forbidden: false };
  }

  return { headers, forbidden: true };
}

export function safePath(base: string, requested: string): string | null {
  const resolved = resolve(base, requested);
  const normalizedBase = normalize(base + sep);

  if (!resolved.startsWith(normalizedBase)) {
    return null;
  }

  return resolved;
}

async function serveFileResponse(path: string, headers: Headers): Promise<Response> {
  const asset = file(path);
  if (!(await asset.exists())) {
    return new Response('Not found', { status: 404, headers });
  }

  return new Response(asset, { headers });
}

async function proxyUpstream(endpoint: string, config: ServerConfig, headers: Headers): Promise<Response> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), config.upstreamTimeoutMs);

  try {
    const upstream = await config.fetchFn(`${config.upstreamUrl}${endpoint}`, {
      method: 'GET',
      headers: { Accept: 'application/json' },
      signal: controller.signal
    });

    const responseHeaders = new Headers(headers);
    responseHeaders.set('Cache-Control', 'no-store');
    responseHeaders.set('X-Content-Type-Options', 'nosniff');
    const disposition = upstream.headers.get('content-disposition');
    if (disposition) responseHeaders.set('Content-Disposition', disposition);
    const contentType = upstream.headers.get('content-type');
    if (contentType) {
      responseHeaders.set('Content-Type', contentType);
    }

    // Keep the deadline active until the body has arrived, not just headers.
    const body = await upstream.arrayBuffer();
    return new Response(body, {
      status: upstream.status,
      statusText: upstream.statusText,
      headers: responseHeaders
    });
  } catch (error) {
    const err = error as Error;
    const timedOut = err.name === 'AbortError';
    const status = timedOut ? 504 : 502;
    const message = timedOut
      ? `Upstream request timed out after ${config.upstreamTimeoutMs}ms`
      : `Failed to reach upstream API: ${err.message}`;

    const responseHeaders = new Headers(headers);
    responseHeaders.set('Content-Type', 'application/json');

    return new Response(JSON.stringify({ error: message }), {
      status,
      headers: responseHeaders
    });
  } finally {
    clearTimeout(timeout);
  }
}

export function createFetchHandler(config = createServerConfig()) {
  return async function fetchHandler(req: Request): Promise<Response> {
    const url = new URL(req.url);
    const requestOrigin = req.headers.get('Origin') || '';
    const { headers: corsHeaders, forbidden } = buildCorsHeaders(url, requestOrigin, config.allowedOrigins);

    if (forbidden) {
      return new Response('Forbidden', { status: 403, headers: corsHeaders });
    }

    if (req.method === 'OPTIONS') {
      return new Response(null, { status: 204, headers: corsHeaders });
    }

    if (req.method !== 'GET') {
      return new Response('Method not allowed', { status: 405, headers: corsHeaders });
    }

    if (url.pathname === '/' || url.pathname === '/index.html') {
      const responseHeaders = new Headers(corsHeaders);
      responseHeaders.set('Content-Type', 'text/html; charset=utf-8');
      return serveFileResponse(resolve(import.meta.dir, 'index.html'), responseHeaders);
    }

    if (url.pathname === '/dist/perf-cascade.min.js') {
      const cascadeJs = safePath(config.nodeModulesDir, 'perf-cascade/dist/perf-cascade.min.js');
      if (!cascadeJs) {
        return new Response('Forbidden', { status: 403, headers: corsHeaders });
      }
      return serveFileResponse(cascadeJs, corsHeaders);
    }

    if (url.pathname.startsWith('/dist/')) {
      const relPath = url.pathname.replace(/^\/dist\//, '');
      const safeDistPath = safePath(config.distDir, relPath);
      if (!safeDistPath) {
        return new Response('Forbidden', { status: 403, headers: corsHeaders });
      }
      return serveFileResponse(safeDistPath, corsHeaders);
    }

    if (
      url.pathname === '/api/traces' ||
      url.pathname === '/api/spans' ||
      url.pathname === '/api/exceptions' ||
      url.pathname === '/api/services' ||
      url.pathname === '/api/insights' ||
      url.pathname === '/api/explore' ||
      url.pathname === '/api/investigation'
    ) {
      return proxyUpstream(`${url.pathname}${url.search}`, config, corsHeaders);
    }

    if (/^\/api\/traces\/[^/]+\/spans$/.test(url.pathname)) {
      return proxyUpstream(`${url.pathname}${url.search}`, config, corsHeaders);
    }

    return new Response('Not found', {
      status: 404,
      headers: corsHeaders
    });
  };
}

export function startServer(config = createServerConfig()) {
  const parsedPort = Number.parseInt(process.env.PORT || '3000', 10);
  const port = Number.isNaN(parsedPort) ? 3000 : parsedPort;

  const server = serve({
    port,
    hostname: process.env.GOTEL_WEB_HOST || '127.0.0.1',
    fetch: createFetchHandler(config)
  });

  console.log(`GoTel Web UI running on http://${server.hostname}:${port}`);
  console.log(`Upstream API: ${config.upstreamUrl}`);
  console.log(
    config.allowedOrigins.length > 0
      ? `Allowed cross-origin browser origins: ${config.allowedOrigins.join(', ')}`
      : 'Cross-origin browser access disabled by default (same-origin only).'
  );

  return server;
}

if (import.meta.main) {
  startServer();
}
