import { describe, expect, it } from 'bun:test';
import {
  buildImportedBundleData,
  buildImportedExploreResult,
  importedTraceSummaries,
  validateInvestigationBundle
} from './bundles';
import type { InvestigationBundle } from './insights-types';

const bundle: InvestigationBundle = {
  format: 'gotel-investigation',
  version: 1,
  created_at: '2026-09-20T10:11:12Z',
  redaction: 'allowlist only',
  filters: {
    from: 1_700_000_000,
    to: 1_700_003_600,
    scope: 'requests',
    service: 'api'
  },
  traces: [
    {
      trace_id: '0123456789abcdef0123456789abcdef',
      spans: [
        {
          trace_id: '0123456789abcdef0123456789abcdef',
          span_id: '0123456789abcdef',
          service_name: 'api',
          span_name: 'GET /items',
          start_time_unix_nano: '1700000000000000000',
          end_time_unix_nano: '1700000000500000000',
          duration_ms: 500,
          status: { code: 1 },
          attributes: { 'http.status_code': 200 }
        }
      ]
    }
  ],
  insights: {
    from: 1_700_000_000,
    to: 1_700_003_600,
    scope: 'requests',
    comparison_available: false,
    summary: {
      count: 1,
      error_count: 0,
      duration_sum_ms: 500,
      rate_per_second: 0.01,
      error_rate: 0,
      p50_ms: 500,
      p95_ms: 500,
      p99_ms: 500,
      histogram: [0, 0, 0, 1, 0, 0]
    },
    buckets: [
      {
        from: 1_700_000_000,
        to: 1_700_000_600,
        trace_id: '0123456789abcdef0123456789abcdef',
        count: 1,
        error_count: 0,
        duration_sum_ms: 500,
        rate_per_second: 0.01,
        error_rate: 0,
        p50_ms: 500,
        p95_ms: 500,
        p99_ms: 500,
        histogram: [0, 0, 0, 1, 0, 0]
      }
    ],
    operations: [
      {
        service: 'api',
        operation: 'GET /items',
        trace_id: '0123456789abcdef0123456789abcdef',
        count: 1,
        error_count: 0,
        duration_sum_ms: 500,
        rate_per_second: 0.01,
        error_rate: 0,
        p50_ms: 500,
        p95_ms: 500,
        p99_ms: 500,
        histogram: [0, 0, 0, 1, 0, 0],
        previous_p95_ms: null,
        change_percent: null
      }
    ],
    errors: [],
    services: [
      { service: 'api', last_seen: 1_700_000_100, in_window: true, versions: ['1.0.0'] }
    ],
    agents: [],
    scanned_spans: 1,
    max_spans: 20_000
  }
};

describe('bundle validation and local import', () => {
  it('validates a correct investigation bundle', () => {
    const result = validateInvestigationBundle(bundle);
    expect(result.ok).toBe(true);
  });

  it('rejects duplicate trace ids', () => {
    const duplicate = {
      ...bundle,
      traces: [...bundle.traces, bundle.traces[0]!]
    };
    const result = validateInvestigationBundle(duplicate);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error).toContain('Duplicate trace_id');
    }
  });

  it('builds imported trace maps and summaries locally', () => {
    const data = buildImportedBundleData(bundle);
    const summaries = importedTraceSummaries(data);

    expect(data.traces).toHaveLength(1);
    expect(data.traceMap.get('0123456789abcdef0123456789abcdef')).toHaveLength(1);
    expect(summaries[0]?.service_name).toBe('api');
    expect(summaries[0]?.duration_ms).toBe(500);
  });
});

it('accepts real empty buckets, fractional last-seen timestamps and unknown provider', () => {
  const value = structuredClone(bundle);
  value.insights.buckets.push({ ...value.insights.buckets[0]!, count: 0, trace_id: '' });
  value.insights.services[0]!.last_seen += .123456;
  value.insights.agents.push({service:'api', model:'m', provider:'', operation:'chat', count:1, error_count:0, tool_calls:0, tool_failures:0, retries:0, retry_samples:0, input_tokens:0, output_tokens:0, token_samples:0, input_token_samples:0, output_token_samples:0, cost_usd:0, cost_samples:0, p50_ms:1, p95_ms:1, trace_id:value.traces[0]!.trace_id});
  expect(validateInvestigationBundle(value).ok).toBe(true);
});

it('rejects malformed IDs, timestamps and excessive span counts', () => {
  for (const change of [
    (v: InvestigationBundle) => { v.traces[0]!.spans[0]!.span_id = '<script>'; },
    (v: InvestigationBundle) => { v.traces[0]!.spans[0]!.start_time_unix_nano = '9223372036854775808'; },
    (v: InvestigationBundle) => { v.traces[0]!.spans = Array(20001).fill(v.traces[0]!.spans[0]); }
  ]) { const v = structuredClone(bundle); change(v); expect(validateInvestigationBundle(v).ok).toBe(false); }
});

it('shows every included trace initially and matches child span time in bucket drilldown', () => {
  const v = structuredClone(bundle);
  const root = v.traces[0]!.spans[0]!;
  root.start_time_unix_nano = '1699999999000000000';
  const child = {...root, span_id:'0000000000000002', parent_span_id:root.span_id, start_time_unix_nano:'1700000001123456789'};
  v.traces[0]!.spans.push(child);
  const data = buildImportedBundleData(v);
  expect(data.traces[0]!.start_time).toBe(1699999999000000000);
  expect(buildImportedExploreResult(data, v.filters).traces).toHaveLength(1);
  expect(buildImportedExploreResult(data, {...v.filters, from:1700000001, to:1700000002}).traces).toHaveLength(1);
  expect(buildImportedExploreResult(data, {...v.filters, from:1700000002, to:1700000003}).traces).toHaveLength(0);
});

it('uses exact nanoseconds at imported bucket boundaries', () => {
 const v=structuredClone(bundle);
 v.traces[0]!.spans[0]!.start_time_unix_nano='1700000000999999999';
 const data=buildImportedBundleData(v);
 expect(buildImportedExploreResult(data,{...v.filters,from:1700000000,to:1700000001}).traces).toHaveLength(1);
 expect(buildImportedExploreResult(data,{...v.filters,from:1700000001,to:1700000002}).traces).toHaveLength(0);
});
