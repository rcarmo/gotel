import { describe, expect, it } from 'bun:test';
import { h } from 'preact';
import { render } from 'preact-render-to-string';
import { AgentsView } from './components/AgentsView';
import { ExploreView } from './components/ExploreView';
import { FiltersBar } from './components/FiltersBar';
import { MetricsView } from './components/MetricsView';
import { OverviewView } from './components/OverviewView';
import { TimelineView } from './components/TimelineView';
import type { ExploreResult, Insights } from './insights-types';

const mockInsights: Insights = {
  from: 1_700_000_000,
  to: 1_700_003_600,
  scope: 'requests',
  comparison_available: true,
  summary: {
    count: 12,
    error_count: 3,
    duration_sum_ms: 2_200,
    rate_per_second: 0.2,
    error_rate: 0.25,
    p50_ms: 120,
    p95_ms: 950,
    p99_ms: 1_400,
    histogram: [1, 2, 3, 4, 1, 1]
  },
  buckets: [
    {
      from: 1_700_000_000,
      to: 1_700_000_600,
      trace_id: 'trace-a',
      count: 5,
      error_count: 2,
      duration_sum_ms: 1_000,
      rate_per_second: 0.1,
      error_rate: 0.4,
      p50_ms: 100,
      p95_ms: 800,
      p99_ms: 900,
      histogram: [1, 1, 1, 1, 1, 0]
    }
  ],
  operations: [
    {
      service: 'api',
      operation: 'GET /health',
      trace_id: 'trace-a',
      count: 4,
      error_count: 1,
      duration_sum_ms: 800,
      rate_per_second: 0.05,
      error_rate: 0.25,
      p50_ms: 100,
      p95_ms: 900,
      p99_ms: 1_000,
      histogram: [0, 1, 1, 1, 1, 0],
      previous_p95_ms: 450,
      change_percent: 100
    }
  ],
  errors: [
    {
      service: 'api',
      operation: 'GET /health',
      type: 'timeout',
      count: 2,
      first_seen: 1_700_000_000,
      last_seen: 1_700_000_300,
      trace_id: 'trace-a',
      previous_count: 0,
      versions: ['1.2.3']
    }
  ],
  services: [
    {
      service: 'api',
      last_seen: 1_700_000_300,
      in_window: true,
      versions: ['1.2.3']
    }
  ],
  agents: [
    {
      service: 'agent-svc',
      model: 'gpt-test',
      operation: 'chat.run',
      provider: 'openai',
      count: 3,
      error_count: 1,
      tool_calls: 5,
      tool_failures: 1,
      retries: 2,
      retry_samples: 2,
      input_tokens: 0,
      output_tokens: 0,
      token_samples:0, input_token_samples:0, output_token_samples:0,
      cost_usd: 0,
      cost_samples: 0,
      p50_ms: 200,
      p95_ms: 900,
      trace_id: 'trace-agent'
    }
  ],
  scanned_spans: 12,
  max_spans: 20_000
};

const mockExplore: ExploreResult = {
  traces: [
    {
      trace_id: 'trace-a',
      service_name: 'api',
      span_name: 'GET /health',
      status_code: 2,
      span_count: 8,
      duration_ms: 950,
      start_time: 1_700_000_100
    }
  ],
  has_more: false
};

const mockSpans = [
  {
    trace_id: 'trace-a',
    span_id: 'root',
    parent_span_id: null,
    service_name: 'api',
    span_name: 'GET /health',
    start_time: 1_700_000_100_000_000,
    end_time: 1_700_000_900_000_000,
    duration_ms: 800,
    status_code: 2
  },
  {
    trace_id: 'trace-a',
    span_id: 'child',
    parent_span_id: 'root',
    service_name: 'db',
    span_name: 'SELECT 1',
    start_time: 1_700_000_150_000_000,
    end_time: 1_700_000_250_000_000,
    duration_ms: 100,
    status_code: 1
  }
];

describe('new frontend components', () => {
  it('renders the overview with native insight sections', () => {
    const result = render(h(OverviewView, {
      insights: mockInsights,
      onSelectTrace: () => {},
      onOpenBucket: () => {}
    }));

    expect(result).toContain('Native overview');
    expect(result).toContain('Error groups vs previous window');
    expect(result).toContain('Slow operations');
    expect(result).toContain('Services last seen');
    expect(result).toContain('New vs previous window');
  });

  it('renders trace search with export controls', () => {
    const result = render(h(ExploreView, {
      result: mockExplore,
      selectedTraceId: 'trace-a',
      selectedTraceIds: new Set(['trace-a']),
      onSelectTrace: () => {},
      onToggleTrace: () => {},
      onExportSelected: () => {}
    }));

    expect(result).toContain('Investigation export');
    expect(result).toContain('Export selected traces');
    expect(result).toContain('GET /health');
  });

  it('renders imported trace search without network wording', () => {
    const result = render(h(ExploreView, {
      result: mockExplore,
      imported: true,
      selectedTraceId: null,
      selectedTraceIds: new Set<string>(),
      onSelectTrace: () => {},
      onToggleTrace: () => {}
    }));

    expect(result).toContain('Imported investigation bundle');
    expect(result).toContain('No network requests are made while inspecting it');
  });

  it('renders agent metrics with unknown token and cost fields', () => {
    const result = render(h(AgentsView, {
      insights: mockInsights,
      onSelectTrace: () => {}
    }));

    expect(result).toContain('Provider / model / operation view');
    expect(result).toContain('unknown');
    expect(result).toContain('Prompt and response bodies are intentionally omitted here');
  });

  it('renders filter inputs for shared native queries', () => {
    const result = render(h(FiltersBar, {
      filters: { from: 1_700_000_000, to: 1_700_003_600, scope: 'requests' },
      timePreset: '24h',
      services: ['api', 'db'],
      onChange: () => {},
      onTimePresetChange: () => {},
      onApply: () => {},
      onReset: () => {}
    }));

    expect(result).toContain('Shared filters');
    expect(result).toContain('http.method');
    expect(result).toContain('requests (root/server/consumer)');
    expect(result).toContain('Apply filters');
  });

  it('renders sampled metrics and bucket drilldown affordances', () => {
    const result = render(h(MetricsView, {
      insights: mockInsights,
      onOpenBucket: () => {}
    }));

    expect(result).toContain('Sampled span metrics');
    expect(result).toContain('Latency histogram');
    expect(result).toContain('Open matching traces');
  });

  it('renders a real trace timeline without synthetic placeholders', () => {
    const result = render(h(TimelineView, {
      selectedTraceId: 'trace-a',
      spans: mockSpans
    }));

    expect(result).toContain('trace-a');
    expect(result).toContain('Span Details (2 spans');
    expect(result).toContain('GET /health');
  });

  it('renders explicit timeline errors', () => {
    const result = render(h(TimelineView, {
      selectedTraceId: 'trace-a',
      spans: [],
      errorMessage: 'bundle trace missing'
    }));

    expect(result).toContain('Unable to Load Trace Timeline');
    expect(result).toContain('bundle trace missing');
  });
});
