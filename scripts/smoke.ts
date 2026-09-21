/**
 * @description Smoke-test the built collector and web UI with isolated data and ephemeral ports.
 * @usage bun run scripts/smoke.ts
 */
import { mkdtemp, rm } from 'node:fs/promises';
import { openSync, closeSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { validateInvestigationBundle, buildImportedBundleData } from '../web/bundles';

const root = resolve(import.meta.dir, '..');
const dir = await mkdtemp(join(tmpdir(), 'gotel-smoke-'));
const children: Bun.Subprocess[] = [];
const logs: string[] = [];
function freePort(): number {
  const listener = Bun.listen({ hostname: '127.0.0.1', port: 0, socket: { data() {} } });
  const port = listener.port;
  listener.stop(true);
  return port;
}
const queryPort = freePort(), otlpPort = freePort(), webPort = freePort();
const api = `http://127.0.0.1:${queryPort}`, web = `http://127.0.0.1:${webPort}`;
function check(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message);
}
async function getJSON(url: string): Promise<any> {
  const r = await fetch(url, { signal: AbortSignal.timeout(2000) });
  check(r.ok, `${url}: HTTP ${r.status} ${await (!r.ok ? r.text() : Promise.resolve(''))}`);
  return r.json();
}
async function eventually(checkReady: () => Promise<boolean>, message: string) {
  for (let i = 0; i < 100; i++) {
    try { if (await checkReady()) return; } catch { /* startup */ }
    await Bun.sleep(100);
  }
  throw new Error(message);
}
function spawn(command: string[], name: string, env: Record<string, string | undefined>) {
  const path = join(dir, `${name}.log`);
  logs.push(path);
  const fd = openSync(path, 'w');
  const child = Bun.spawn(command, { cwd: root, env, stdout: fd, stderr: fd });
  closeSync(fd);
  children.push(child);
  return child;
}
try {
  const config = join(dir, 'config.yaml');
  await Bun.write(config, `receivers:
  otlp:
    protocols:
      http:
        endpoint: 127.0.0.1:${otlpPort}
processors:
  batch:
    timeout: 100ms
exporters:
  sqlite:
    query_port: ${queryPort}
    cleanup_interval: 1h
service:
  telemetry:
    metrics:
      level: none
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [sqlite]
`);
  const env = {
    ...process.env,
    GOTEL_CONFIG: '', OTEL_CONFIG_FILE: '',
    GOTEL_DB_PATH: join(dir, 'gotel.db'),
    GOTEL_QUERY_HOST: '127.0.0.1',
    GOTEL_WEB_HOST: '127.0.0.1',
    GOTEL_RETENTION: '1h', GOTEL_ALLOWED_ORIGINS: ''
  };
  spawn([join(root, 'gotel'), '--config', config], 'collector', env);
  await eventually(async () => (await fetch(`${api}/ready`)).ok, 'Collector did not become ready');
  spawn([process.execPath, 'run', join(root, 'web/server.ts')], 'web', { ...env, PORT: String(webPort), GOTEL_API_URL: api });
  await eventually(async () => (await fetch(web)).ok, 'Web server did not become ready');

  // First requests must not depend on a previous successful trace-list request.
  const first = await Promise.all([getJSON(`${web}/api/spans`), getJSON(`${web}/api/exceptions`)]);
  check(first.every(x => Array.isArray(x) && x.length === 0), 'Synthetic records on initial empty load');
  for (const asset of ['/dist/index.js', '/dist/bundle.css', '/dist/perf-cascade.min.js']) {
    const r = await fetch(web + asset);
    check(r.ok && (await r.text()).length > 100, `Missing built asset ${asset}`);
  }
  check((await fetch(web + '/dist/missing.js')).status === 404, 'Missing asset must return 404');
  for (const url of [api + '/api/traces', web + '/api/traces']) {
    check((await fetch(url, { headers: { Origin: 'https://untrusted.example' } })).status === 403, 'Cross-origin access allowed');
  }

  const traceId = '0102030405060708090a0b0c0d0e0f10';
  const start = BigInt(Date.now()) * 1_000_000n + 123n;
  const spans = Array.from({ length: 125 }, (_, i) => ({
    traceId,
    spanId: (i + 1).toString(16).padStart(16, '0'),
    ...(i ? { parentSpanId: '0000000000000001' } : {}),
    name: 'smoke-operation', kind: 2,
    startTimeUnixNano: String(start + BigInt(i) * 1_000_000n),
    endTimeUnixNano: String(start + BigInt(i + 1) * 1_000_000n),
    status: { code: i === 0 ? 2 : 1 },
    attributes: i === 0 ? [
      { key: 'gen_ai.request.model', value: { stringValue: 'smoke-model' } },
      { key: 'gen_ai.usage.input_tokens', value: { intValue: '123' } },
      { key: 'gen_ai.prompt', value: { stringValue: 'SECRET-SMOKE-PROMPT' } }
    ] : [],
    events: i === 0 ? [{ name: 'exception', timeUnixNano: String(start + 17n), attributes: [{ key: 'exception.message', value: { stringValue: 'smoke exception' } }] }] : []
  }));
  const resourceSpans = [spans.slice(0, 100), spans.slice(100)].map((spans, i) => ({
    resource: { attributes: [
      { key: 'service.name', value: { stringValue: 'gotel-smoke' } },
      { key: 'service.instance.id', value: { stringValue: `instance-${i}` } }
    ] },
    scopeSpans: [{ scope: { name: 'smoke', version: '1.0' }, spans }]
  }));
  const ingest = await fetch(`http://127.0.0.1:${otlpPort}/v1/traces`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ resourceSpans })
  });
  check(ingest.ok, `OTLP ingestion failed: ${await ingest.text()}`);
  await eventually(async () => (await getJSON(`${api}/api/status`)).span_count === 125, 'Spans were not persisted');
  const raw = await getJSON(`${web}/api/traces/${traceId}/spans`);
  check(raw.length === 125, 'Per-trace spans were truncated');
  const trace = await getJSON(`${api}/api/traces/${traceId}`);
  check(trace.resourceSpans.length === 2, 'Resource identity was lost');
  const returned = trace.resourceSpans.flatMap((rs: any) => rs.scopeSpans.flatMap((ss: any) => ss.spans));
  check(returned.find((s: any) => s.spanId === '0000000000000001').startTimeUnixNano === String(start), 'Timestamp precision lost');
  check((await getJSON(`${web}/api/exceptions`)).length === 1, 'Exception not returned');
  check((await getJSON(`${api}/render?target=otel.*&from=0&until=1`)).length === 0, 'Graphite ignored time range');
  check((await getJSON(`${api}/render?target=otel.*&from=-1h`)).length > 0, 'Graphite omitted current metrics');
  const from = Number(start / 1_000_000_000n) - 3600, to = Number(start / 1_000_000_000n) + 2;
  const filters = `from=${from}&to=${to}&scope=all`;
  const insights = await getJSON(`${web}/api/insights?${filters}`);
  check(insights.summary.count === 125 && insights.summary.error_count === 1 && insights.summary.p95_ms === 1, 'Native aggregate mismatch');
  check(insights.agents[0]?.input_tokens === 123 && insights.agents[0]?.cost_samples === 0, 'Agent interpretation mismatch');
  check(insights.buckets.reduce((sum: number, b: any) => sum + b.count, 0) === 125, 'Bucket count mismatch');
  const explored = await getJSON(`${web}/api/explore?${filters}&service=gotel-smoke&status=error`);
  check(explored.traces[0]?.span_count === 125, 'Native search lost complete trace context');
  const bundle = await getJSON(`${web}/api/investigation?${filters}&trace_id=${traceId}`);
  const validation = validateInvestigationBundle(bundle);
  check(validation.ok, `Live bundle rejected by frontend: ${validation.ok ? '' : validation.error}`);
  check(!JSON.stringify(bundle).includes('SECRET-SMOKE-PROMPT'), 'Secret leaked in export');
  check(bundle.traces[0].spans[0].start_time_unix_nano === String(start), 'Bundle nanosecond precision lost');
  check(buildImportedBundleData(bundle).traceMap.get(traceId)?.length === 125, 'Local bundle import lost spans');
  const bundlePath = join(dir, 'investigation.json');
  await Bun.write(bundlePath, JSON.stringify(bundle));
  if (process.env.GOTEL_BROWSER_SMOKE === '1') {
    const { browserSmoke } = await import('./browser-smoke');
    await browserSmoke(web, traceId, bundlePath);
  }
  console.log('Smoke passed: startup, OTLP fidelity, 125 complete spans, Graphite ranges, native insights/search/agent metrics, redacted export and local import, web assets and origin policy.');
} catch (error) {
  for (const path of logs) console.error(await Bun.file(path).text());
  throw error;
} finally {
  for (const child of children.reverse()) {
    child.kill('SIGTERM');
    const timer = setTimeout(() => child.kill('SIGKILL'), 5000);
    await child.exited;
    clearTimeout(timer);
  }
  await rm(dir, { recursive: true, force: true });
}
