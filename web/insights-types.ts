// Native Gotel API. All query times are Unix seconds; windows are [from, to).
export interface InsightFilters {
  from: number; to: number; service?: string; operation?: string;
  status?: 'error'; min_duration_ms?: number; max_duration_ms?: number;
  attribute?: string; value?: string; scope?: 'requests' | 'all';
}
export interface Distribution {
  count: number; error_count: number; duration_sum_ms: number;
  rate_per_second: number; error_rate: number; // fraction, not percentage
  p50_ms: number; p95_ms: number; p99_ms: number;
  histogram: number[]; // <10, <50, <100, <500, <1000, >=1000 ms
}
export interface InsightBucket extends Distribution { from: number; to: number; trace_id: string; }
export interface InsightOperation extends Distribution {
  service: string; operation: string; trace_id: string;
  previous_p95_ms: number | null; change_percent: number | null;
}
export interface InsightError {
  service: string; operation: string; type: string; count: number;
  first_seen: number; last_seen: number; trace_id: string;
  previous_count: number | null; versions: string[];
}
export interface InsightService { service: string; last_seen: number; in_window: boolean; versions: string[]; }
export interface AgentRow {
  service: string; model: string; operation: string; provider: string;
  count: number; error_count: number; tool_calls: number; tool_failures: number;
  retries: number; retry_samples: number; input_tokens: number; output_tokens: number;
  token_samples: number; input_token_samples: number; output_token_samples: number;
  cost_usd: number; cost_samples: number;
  p50_ms: number; p95_ms: number; trace_id: string;
}
export interface Insights {
  from: number; to: number; scope: 'requests' | 'all'; comparison_available: boolean;
  summary: Distribution; buckets: InsightBucket[]; operations: InsightOperation[];
  errors: InsightError[]; services: InsightService[]; agents: AgentRow[];
  scanned_spans: number; max_spans: number;
}
export interface ExploreTrace {
  trace_id: string; service_name: string; span_name: string; status_code: number;
  span_count: number; duration_ms: number; start_time: number; // Unix nanoseconds
}
export interface ExploreResult { traces: ExploreTrace[]; has_more: boolean; }
// Export uses an allowlist. No prompts/responses, arbitrary attributes, messages,
// stacks, event payloads, baggage or scope metadata. Names may still be sensitive.
export interface BundleSpan {
  trace_id: string; span_id: string; parent_span_id?: string;
  service_name: string; span_name: string; kind?: string;
  start_time_unix_nano: string; end_time_unix_nano: string;
  duration_ms: number; status: { code: number };
  resource?: Record<string, string>; attributes?: Record<string, string | number>;
  links?: { trace_id: string; span_id: string }[];
}
export interface InvestigationBundle {
  format: 'gotel-investigation'; version: 1; created_at: string;
  redaction: string; filters: InsightFilters;
  traces: { trace_id: string; spans: BundleSpan[] }[];
  insights: Insights;
}
// GET /api/insights?from=&to=&... : Insights, max 20k spans in each window;
// 422 (never partial aggregates) on overflow, 400 for invalid filters.
// GET /api/explore?same_filters&limit=100 : ExploreResult, max limit 200.
// GET /api/investigation?same_filters&trace_id=<id>&trace_id=<id> : bundle,
// max 20 explicitly selected complete traces / 20k spans total; errors, not truncation.
// GET /api/services remains available for service discovery.
// Import is browser-local, read-only. Never uploads or modifies the live store.
