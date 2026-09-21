import { describe, expect, it } from 'bun:test';
import { createFetchHandler, type ServerConfig } from './server';

function createConfig(overrides: Partial<ServerConfig> = {}): ServerConfig {
  return {
    allowedOrigins: [],
    upstreamUrl: 'http://upstream.test',
    upstreamTimeoutMs: 1000,
    distDir: '/tmp/gotel-web-test-dist',
    nodeModulesDir: '/tmp/gotel-web-test-node-modules',
    fetchFn: fetch,
    ...overrides
  };
}

describe('web server proxy', () => {
  it('proxies selected trace span requests to the backend endpoint', async () => {
    const calls: string[] = [];
    const handler = createFetchHandler(createConfig({
      fetchFn: async (input) => {
        calls.push(String(input));
        return new Response(JSON.stringify([{ trace_id: 'trace-123', span_id: 'span-1' }]), {
          headers: { 'Content-Type': 'application/json' }
        });
      }
    }));

    const response = await handler(new Request('http://localhost:3000/api/traces/trace-123/spans'));

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual([{ trace_id: 'trace-123', span_id: 'span-1' }]);
    expect(calls).toEqual(['http://upstream.test/api/traces/trace-123/spans']);
  });

  it('proxies native insights, explore and investigation endpoints with query strings', async () => {
    const calls: string[] = [];
    const handler = createFetchHandler(createConfig({
      fetchFn: async (input) => {
        calls.push(String(input));
        return new Response(JSON.stringify({ ok: true }), {
          headers: { 'Content-Type': 'application/json' }
        });
      }
    }));

    await handler(new Request('http://localhost:3000/api/insights?from=1&to=2'));
    await handler(new Request('http://localhost:3000/api/explore?from=1&to=2&limit=100'));
    await handler(new Request('http://localhost:3000/api/investigation?from=1&to=2&trace_id=a'));

    expect(calls).toEqual([
      'http://upstream.test/api/insights?from=1&to=2',
      'http://upstream.test/api/explore?from=1&to=2&limit=100',
      'http://upstream.test/api/investigation?from=1&to=2&trace_id=a'
    ]);
  });

  it('returns a real upstream error instead of synthetic telemetry records', async () => {
    const handler = createFetchHandler(createConfig({
      fetchFn: async () => {
        throw new Error('connect ECONNREFUSED');
      }
    }));

    const response = await handler(new Request('http://localhost:3000/api/insights'));
    const payload = await response.json() as { error?: string };

    expect(response.status).toBe(502);
    expect(payload.error).toContain('connect ECONNREFUSED');
  });
});

describe('web server CORS policy', () => {
  it('defaults to same-origin access without wildcard CORS headers', async () => {
    const handler = createFetchHandler(createConfig({
      fetchFn: async () => new Response(JSON.stringify([{ name: 'svc' }]), {
        headers: { 'Content-Type': 'application/json' }
      })
    }));

    const response = await handler(new Request('http://localhost:3000/api/services', {
      headers: { Origin: 'http://localhost:3000' }
    }));

    expect(response.status).toBe(200);
    expect(response.headers.get('Access-Control-Allow-Origin')).toBeNull();
  });

  it('allows explicitly configured cross-origin browser requests', async () => {
    const handler = createFetchHandler(createConfig({
      allowedOrigins: ['https://ui.example.com'],
      fetchFn: async () => new Response(JSON.stringify([{ name: 'svc' }]), {
        headers: { 'Content-Type': 'application/json' }
      })
    }));

    const response = await handler(new Request('http://localhost:3000/api/services', {
      headers: { Origin: 'https://ui.example.com' }
    }));

    expect(response.status).toBe(200);
    expect(response.headers.get('Access-Control-Allow-Origin')).toBe('https://ui.example.com');
  });

  it('rejects non-configured cross-origin browser requests', async () => {
    const handler = createFetchHandler(createConfig());

    const response = await handler(new Request('http://localhost:3000/api/services', {
      headers: { Origin: 'https://evil.example.com' }
    }));

    expect(response.status).toBe(403);
  });
});
