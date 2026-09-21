import type {
  AgentRow,
  BundleSpan,
  ExploreResult,
  ExploreTrace,
  InsightBucket,
  InsightError,
  InsightFilters,
  InsightOperation,
  InsightService,
  Insights,
  InvestigationBundle
} from './insights-types';
import { normalizeSpan, type Span, type StoredSpan, type TraceSummary } from './state';

export interface ImportedBundleData {
  bundle: InvestigationBundle;
  traces: ExploreTrace[];
  traceMap: Map<string, Span[]>;
}

export const INVESTIGATION_BUNDLE_BYTE_LIMIT = 32 << 20;
const INVESTIGATION_BUNDLE_TRACE_LIMIT = 20;
const INVESTIGATION_BUNDLE_SPAN_LIMIT = 20_000;
const INVESTIGATION_SERVICE_LIMIT = 1_000;
const INVESTIGATION_BUCKET_LIMIT = 121;
const FILTER_STRING_LIMIT = 1_024;
const MAX_INT64 = 9_223_372_036_854_775_807n;
const MAX_QUERY_SECONDS = 9_223_372_035;
const MAX_WINDOW_SECONDS = 7 * 24 * 60 * 60;
const TRACE_ID_PATTERN = /^[0-9a-f]{32}$/;
const SPAN_ID_PATTERN = /^[0-9a-f]{16}$/;

type ValidationResult<T> =
  | { ok: true; value: T }
  | { ok: false; error: string };

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value);
}

function isNonNegativeFiniteNumber(value: unknown): value is number {
  return isFiniteNumber(value) && value >= 0;
}

function isString(value: unknown): value is string {
  return typeof value === 'string';
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === 'string' && value.trim().length > 0;
}

function isTimestampSeconds(value: unknown): value is number {
  return isFiniteNumber(value) && Number.isInteger(value) && value >= 0 && value <= MAX_QUERY_SECONDS;
}

function isTimestampNanos(value: unknown): value is string {
  if (!isString(value) || !/^\d+$/.test(value)) {
    return false;
  }

  try {
    const parsed = BigInt(value);
    return parsed >= 0n && parsed <= MAX_INT64;
  } catch {
    return false;
  }
}

function isValidTraceId(value: unknown, allowEmpty = false): value is string {
  return typeof value === 'string' && (value === '' ? allowEmpty : TRACE_ID_PATTERN.test(value));
}

function isValidSpanId(value: unknown, allowEmpty = false): value is string {
  return typeof value === 'string' && (value === '' ? allowEmpty : SPAN_ID_PATTERN.test(value));
}

function validateStringArray(value: unknown, maxItems = FILTER_STRING_LIMIT): value is string[] {
  return Array.isArray(value) && value.length <= maxItems && value.every((entry) => typeof entry === 'string');
}

function validateJsonValue(value: unknown, depth = 0): boolean {
  if (depth > 6) {
    return false;
  }
  if (value === null) {
    return true;
  }
  if (typeof value === 'string' || typeof value === 'boolean') {
    return true;
  }
  if (typeof value === 'number') {
    return Number.isFinite(value);
  }
  if (Array.isArray(value)) {
    return value.length <= FILTER_STRING_LIMIT && value.every((entry) => validateJsonValue(entry, depth + 1));
  }
  if (isRecord(value)) {
    const entries = Object.entries(value);
    return entries.length <= FILTER_STRING_LIMIT && entries.every(([key, entry]) => key.length <= FILTER_STRING_LIMIT && validateJsonValue(entry, depth + 1));
  }
  return false;
}

function validateFilters(value: unknown): value is InsightFilters {
  if (!isRecord(value)) return false;
  if (!isTimestampSeconds(value.from) || !isTimestampSeconds(value.to) || value.from >= value.to) return false;
  if (value.to - value.from > MAX_WINDOW_SECONDS) return false;
  if (value.service !== undefined && (!isString(value.service) || value.service.length > FILTER_STRING_LIMIT)) return false;
  if (value.operation !== undefined && (!isString(value.operation) || value.operation.length > FILTER_STRING_LIMIT)) return false;
  if (value.status !== undefined && value.status !== 'error') return false;
  if (value.min_duration_ms !== undefined && !isNonNegativeFiniteNumber(value.min_duration_ms)) return false;
  if (value.max_duration_ms !== undefined && !isNonNegativeFiniteNumber(value.max_duration_ms)) return false;
  if (value.min_duration_ms !== undefined && value.max_duration_ms !== undefined && value.min_duration_ms > value.max_duration_ms) return false;
  if (value.attribute !== undefined && (!isString(value.attribute) || value.attribute.length > FILTER_STRING_LIMIT)) return false;
  if (value.value !== undefined && (!isString(value.value) || value.value.length > FILTER_STRING_LIMIT)) return false;
  if ((value.attribute === undefined) !== (value.value === undefined)) return false;
  if (value.scope !== undefined && value.scope !== 'requests' && value.scope !== 'all') return false;
  return true;
}

function validateDistribution(value: unknown): boolean {
  return isRecord(value)
    && isNonNegativeFiniteNumber(value.count)
    && isNonNegativeFiniteNumber(value.error_count)
    && isNonNegativeFiniteNumber(value.duration_sum_ms)
    && isNonNegativeFiniteNumber(value.rate_per_second)
    && isNonNegativeFiniteNumber(value.error_rate)
    && isNonNegativeFiniteNumber(value.p50_ms)
    && isNonNegativeFiniteNumber(value.p95_ms)
    && isNonNegativeFiniteNumber(value.p99_ms)
    && Array.isArray(value.histogram)
    && value.histogram.length === 6
    && value.histogram.every(isNonNegativeFiniteNumber);
}

function validateBucket(value: unknown): value is InsightBucket {
  return validateDistribution(value)
    && isRecord(value)
    && isTimestampSeconds(value.from)
    && isTimestampSeconds(value.to)
    && value.from < value.to
    && isValidTraceId(value.trace_id, true);
}

function validateOperation(value: unknown): value is InsightOperation {
  return validateDistribution(value)
    && isRecord(value)
    && isString(value.service)
    && isString(value.operation)
    && isValidTraceId(value.trace_id)
    && (value.previous_p95_ms === null || isNonNegativeFiniteNumber(value.previous_p95_ms))
    && (value.change_percent === null || isFiniteNumber(value.change_percent));
}

function validateError(value: unknown): value is InsightError {
  return isRecord(value)
    && isString(value.service)
    && isString(value.operation)
    && isNonEmptyString(value.type)
    && isNonNegativeFiniteNumber(value.count)
    && isNonNegativeFiniteNumber(value.first_seen)
    && isNonNegativeFiniteNumber(value.last_seen)
    && value.first_seen <= value.last_seen
    && isValidTraceId(value.trace_id)
    && (value.previous_count === null || isNonNegativeFiniteNumber(value.previous_count))
    && validateStringArray(value.versions);
}

function validateService(value: unknown): value is InsightService {
  return isRecord(value)
    && isString(value.service)
    && isNonNegativeFiniteNumber(value.last_seen)
    && typeof value.in_window === 'boolean'
    && validateStringArray(value.versions);
}

function validateAgentRow(value: unknown): value is AgentRow {
  return isRecord(value)
    && isString(value.service)
    && isString(value.model)
    && isString(value.operation)
    && isString(value.provider)
    && isNonNegativeFiniteNumber(value.count)
    && isNonNegativeFiniteNumber(value.error_count)
    && isNonNegativeFiniteNumber(value.tool_calls)
    && isNonNegativeFiniteNumber(value.tool_failures)
    && isNonNegativeFiniteNumber(value.retries)
    && isNonNegativeFiniteNumber(value.retry_samples)
    && isNonNegativeFiniteNumber(value.input_tokens)
    && isNonNegativeFiniteNumber(value.output_tokens)
    && isNonNegativeFiniteNumber(value.token_samples)
    && isNonNegativeFiniteNumber(value.input_token_samples)
    && isNonNegativeFiniteNumber(value.output_token_samples)
    && isNonNegativeFiniteNumber(value.cost_usd)
    && isNonNegativeFiniteNumber(value.cost_samples)
    && isNonNegativeFiniteNumber(value.p50_ms)
    && isNonNegativeFiniteNumber(value.p95_ms)
    && isValidTraceId(value.trace_id);
}

function validateInsights(value: unknown): value is Insights {
  return isRecord(value)
    && isTimestampSeconds(value.from)
    && isTimestampSeconds(value.to)
    && value.from < value.to
    && (value.scope === 'requests' || value.scope === 'all')
    && typeof value.comparison_available === 'boolean'
    && validateDistribution(value.summary)
    && Array.isArray(value.buckets)
    && value.buckets.length <= INVESTIGATION_BUCKET_LIMIT
    && value.buckets.every(validateBucket)
    && Array.isArray(value.operations)
    && value.operations.length <= INVESTIGATION_BUNDLE_SPAN_LIMIT
    && value.operations.every(validateOperation)
    && Array.isArray(value.errors)
    && value.errors.length <= INVESTIGATION_BUNDLE_SPAN_LIMIT
    && value.errors.every(validateError)
    && Array.isArray(value.services)
    && value.services.length <= INVESTIGATION_SERVICE_LIMIT
    && value.services.every(validateService)
    && Array.isArray(value.agents)
    && value.agents.length <= INVESTIGATION_BUNDLE_SPAN_LIMIT
    && value.agents.every(validateAgentRow)
    && isNonNegativeFiniteNumber(value.scanned_spans)
    && isNonNegativeFiniteNumber(value.max_spans)
    && value.max_spans <= INVESTIGATION_BUNDLE_SPAN_LIMIT;
}

function validateLink(value: unknown): boolean {
  return isRecord(value)
    && isValidTraceId(value.trace_id)
    && isValidSpanId(value.span_id)
    && (value.attributes === undefined || validateJsonValue(value.attributes));
}

function validateSpanRecord(value: unknown, expectedTraceId: string): value is BundleSpan {
  if (!isRecord(value)) return false;
  if (!isValidTraceId(value.trace_id) || value.trace_id !== expectedTraceId) return false;
  if (!isValidSpanId(value.span_id)) return false;
  if (value.parent_span_id !== undefined && value.parent_span_id !== null && !isValidSpanId(value.parent_span_id, true)) return false;
  if (!isString(value.service_name) || !isString(value.span_name)) return false;
  if (value.kind !== undefined && typeof value.kind !== 'string') return false;
  if (!isTimestampNanos(value.start_time_unix_nano) || !isTimestampNanos(value.end_time_unix_nano)) return false;
  if (!isNonNegativeFiniteNumber(value.duration_ms)) return false;
  if (!isRecord(value.status) || !isNonNegativeFiniteNumber(value.status.code)) return false;
  if (value.resource !== undefined && (!isRecord(value.resource) || !validateJsonValue(value.resource))) return false;
  if (value.attributes !== undefined && (!isRecord(value.attributes) || !validateJsonValue(value.attributes))) return false;
  if (value.links !== undefined && (!Array.isArray(value.links) || value.links.length > FILTER_STRING_LIMIT || value.links.some((link) => !validateLink(link)))) return false;
  return true;
}

export function validateInvestigationBundle(value: unknown): ValidationResult<InvestigationBundle> {
  if (!isRecord(value)) {
    return { ok: false, error: 'Bundle must be a JSON object.' };
  }
  if (value.format !== 'gotel-investigation') {
    return { ok: false, error: 'Unsupported bundle format.' };
  }
  if (value.version !== 1) {
    return { ok: false, error: 'Unsupported bundle version.' };
  }
  if (!isNonEmptyString(value.created_at) || Number.isNaN(Date.parse(value.created_at))) {
    return { ok: false, error: 'Bundle created_at must be a valid timestamp.' };
  }
  if (!isNonEmptyString(value.redaction)) {
    return { ok: false, error: 'Bundle redaction description is required.' };
  }
  if (!validateFilters(value.filters)) {
    return { ok: false, error: 'Bundle filters are invalid.' };
  }
  if (!Array.isArray(value.traces) || value.traces.length === 0 || value.traces.length > INVESTIGATION_BUNDLE_TRACE_LIMIT) {
    return { ok: false, error: 'Bundle traces must contain between 1 and 20 traces.' };
  }
  if (!validateInsights(value.insights)) {
    return { ok: false, error: 'Bundle insights are invalid.' };
  }

  const seenTraceIds = new Set<string>();
  let totalSpans = 0;
  for (const trace of value.traces) {
    if (!isRecord(trace) || !isValidTraceId(trace.trace_id)) {
      return { ok: false, error: 'Each exported trace must have a lowercase 32-hex trace_id.' };
    }
    if (seenTraceIds.has(trace.trace_id)) {
      return { ok: false, error: `Duplicate trace_id in bundle: ${trace.trace_id}` };
    }
    seenTraceIds.add(trace.trace_id);
    if (!Array.isArray(trace.spans) || trace.spans.length === 0) {
      return { ok: false, error: `Trace ${trace.trace_id} has no spans.` };
    }
    totalSpans += trace.spans.length;
    if (totalSpans > INVESTIGATION_BUNDLE_SPAN_LIMIT) {
      return { ok: false, error: 'Bundle exceeds the maximum of 20,000 spans.' };
    }
    for (const span of trace.spans) {
      if (!validateSpanRecord(span, trace.trace_id)) {
        return { ok: false, error: `Trace ${trace.trace_id} contains an invalid span record.` };
      }
    }
  }

  return { ok: true, value: value as unknown as InvestigationBundle };
}

function bundleSpanToStoredSpan(span: BundleSpan): StoredSpan {
  return {
    trace_id: span.trace_id,
    span_id: span.span_id,
    parent_span_id: span.parent_span_id,
    service_name: span.service_name,
    span_name: span.span_name,
    kind: span.kind,
    start_time_unix_nano: span.start_time_unix_nano,
    end_time_unix_nano: span.end_time_unix_nano,
    duration_ms: span.duration_ms,
    status: span.status,
    resource: span.resource,
    attributes: span.attributes,
    links: span.links
  };
}

function getRootLikeSpan(spans: Span[]): Span {
  const byId = new Set(spans.map((span) => span.span_id));
  return spans.find((span) => !span.parent_span_id || !byId.has(span.parent_span_id)) ?? spans[0]!;
}

function isRequestLike(span: Span): boolean {
  const kind = span.kind?.toLowerCase();
  return kind === 'server' || kind === 'consumer' || !span.parent_span_id || span.parent_span_id === '0000000000000000';
}

function attributeValueForFilter(value: unknown): string | null {
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
    return String(value);
  }
  if (value === null) {
    return 'null';
  }
  if (Array.isArray(value) || isRecord(value)) {
    try {
      return JSON.stringify(value);
    } catch {
      return null;
    }
  }
  return null;
}

function spanStartsInWindow(span: Span, filters: InsightFilters): boolean {
  const start = BigInt(span.start_time_raw ?? Math.trunc(span.start_time));
  return start >= BigInt(filters.from) * 1_000_000_000n && start < BigInt(filters.to) * 1_000_000_000n;
}

function spanMatchesInsightFilters(span: Span, filters: InsightFilters): boolean {
  if (!spanStartsInWindow(span, filters)) {
    return false;
  }
  if (filters.scope !== 'all' && !isRequestLike(span)) {
    return false;
  }
  if (filters.service !== undefined && span.service_name !== filters.service) {
    return false;
  }
  if (filters.operation !== undefined && span.span_name !== filters.operation) {
    return false;
  }
  if (filters.status === 'error' && span.status_code !== 2) {
    return false;
  }
  if (typeof filters.min_duration_ms === 'number' && (span.duration_ms ?? 0) < filters.min_duration_ms) {
    return false;
  }
  if (typeof filters.max_duration_ms === 'number' && (span.duration_ms ?? 0) > filters.max_duration_ms) {
    return false;
  }
  if (filters.attribute !== undefined) {
    const actual = attributeValueForFilter(span.attributes?.[filters.attribute]);
    if (actual !== (filters.value ?? '')) {
      return false;
    }
  }
  return true;
}

export function traceMatchesInsightFilters(spans: Span[], filters: InsightFilters): boolean {
  return spans.some((span) => spanMatchesInsightFilters(span, filters));
}

function toExploreTrace(traceId: string, spans: Span[]): ExploreTrace {
  const root = getRootLikeSpan(spans);
  const firstStart = spans.reduce((min, span) => Math.min(min, span.start_time), Number.POSITIVE_INFINITY);
  const lastEnd = spans.reduce((max, span) => Math.max(max, span.end_time), 0);
  const errorStatus = spans.some((span) => span.status_code === 2) ? 2 : root.status_code;

  return {
    trace_id: traceId,
    service_name: root.service_name,
    span_name: root.span_name,
    status_code: errorStatus,
    span_count: spans.length,
    duration_ms: Math.max(root.duration_ms ?? 0, (lastEnd - firstStart) / 1_000_000),
    start_time: firstStart
  };
}

export function buildImportedBundleData(bundle: InvestigationBundle): ImportedBundleData {
  const traceMap = new Map<string, Span[]>();
  const traces = bundle.traces.map((trace) => {
    const spans = trace.spans.map((span) => normalizeSpan(bundleSpanToStoredSpan(span)));
    traceMap.set(trace.trace_id, spans);
    return toExploreTrace(trace.trace_id, spans);
  }).sort((a, b) => b.start_time - a.start_time);

  return {
    bundle,
    traces,
    traceMap
  };
}

export function buildImportedExploreResult(data: ImportedBundleData, filters: InsightFilters): ExploreResult {
  const original = data.bundle.filters;
  // A selected trace may start outside the exported context. Show every included
  // trace initially; bucket drilldown uses any included span's event time only.
  // Removed attributes cannot be used to reapply the original query faithfully.
  const fullWindow = filters.from === original.from && filters.to === original.to;
  return {
    traces: fullWindow ? data.traces : data.traces.filter((trace) =>
      (data.traceMap.get(trace.trace_id) ?? []).some(span =>
        spanStartsInWindow(span, filters))),
    has_more: false
  };
}

export function summarizeImportedTrace(traceId: string, spans: Span[]): TraceSummary {
  const root = getRootLikeSpan(spans);
  const startTime = spans.reduce((min, span) => Math.min(min, span.start_time), Number.POSITIVE_INFINITY);
  const endTime = spans.reduce((max, span) => Math.max(max, span.end_time), 0);
  return {
    trace_id: traceId,
    span_name: root.span_name,
    service_name: root.service_name,
    duration_ms: Math.max(root.duration_ms ?? 0, (endTime - startTime) / 1_000_000),
    status_code: spans.some((span) => span.status_code === 2) ? 2 : root.status_code,
    span_count: spans.length,
    start_time: startTime,
    end_time: endTime,
    root_span_id: root.span_id,
    error_count: spans.filter((span) => span.status_code === 2).length
  };
}

export function importedTraceSummaries(data: ImportedBundleData): TraceSummary[] {
  return data.traces.map((trace) => summarizeImportedTrace(trace.trace_id, data.traceMap.get(trace.trace_id) ?? []));
}
