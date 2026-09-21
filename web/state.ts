export interface Span {
  id?: number;
  trace_id: string;
  span_id: string;
  parent_span_id?: string | null;
  service_name: string;
  span_name: string;
  kind?: string;
  start_time: number; // Unix timestamp in nanoseconds (best-effort numeric form)
  end_time: number; // Unix timestamp in nanoseconds (best-effort numeric form)
  start_time_raw?: string; // Preserved raw nanosecond string when available
  end_time_raw?: string; // Preserved raw nanosecond string when available
  duration_ms?: number;
  status_code: SpanStatusCode;
  status_message?: string;
  resource?: Record<string, string>;
  attributes?: Record<string, unknown>;
  events?: SpanEvent[];
  links?: SpanLink[];
  data?: Record<string, unknown>;
}

export interface StoredSpan extends Partial<Span> {
  start_time_unix_nano?: number | string;
  end_time_unix_nano?: number | string;
  status?: { code?: number; message?: string };
}

function parseNanoTimestamp(value: number | string | undefined): { numeric: number; raw?: string } {
  if (typeof value === 'number') {
    return { numeric: Number.isFinite(value) ? value : 0 };
  }

  if (typeof value === 'string') {
    const trimmed = value.trim();
    if (trimmed.length === 0) {
      return { numeric: 0, raw: value };
    }

    const numeric = Number(trimmed);
    return {
      numeric: Number.isFinite(numeric) ? numeric : 0,
      raw: trimmed
    };
  }

  return { numeric: 0 };
}

export function normalizeSpan(raw: StoredSpan): Span {
  const start = raw.start_time_unix_nano ?? raw.start_time;
  const end = raw.end_time_unix_nano ?? raw.end_time;
  const parsedStart = parseNanoTimestamp(start);
  const parsedEnd = parseNanoTimestamp(end);

  return {
    ...raw,
    trace_id: raw.trace_id ?? '',
    span_id: raw.span_id ?? '',
    parent_span_id: raw.parent_span_id ?? null,
    service_name: raw.service_name ?? 'unknown',
    span_name: raw.span_name ?? '',
    kind: raw.kind,
    start_time: parsedStart.numeric,
    end_time: parsedEnd.numeric,
    start_time_raw: parsedStart.raw,
    end_time_raw: parsedEnd.raw,
    duration_ms: raw.duration_ms,
    status_code: raw.status?.code ?? raw.status_code ?? 0,
    status_message: raw.status?.message ?? raw.status_message,
    resource: raw.resource,
    attributes: raw.attributes,
    events: raw.events,
    links: raw.links,
    data: raw.data
  };
}

export interface SpanEvent {
  name: string;
  timestamp: number;
  attributes?: Record<string, unknown>;
}

export interface SpanLink {
  trace_id: string;
  span_id: string;
  attributes?: Record<string, unknown>;
}

export type SpanStatusCode = 0 | 1 | 2 | number;

export interface Exception {
  trace_id: string;
  span_id: string;
  service_name: string;
  span_name: string;
  exception_type?: string;
  message?: string;
  stack_trace?: string;
  timestamp: number;
  severity?: ExceptionSeverity;
  attributes?: Record<string, unknown>;
}

export type ExceptionSeverity = 'critical' | 'warning' | 'info' | 'error' | string;

export interface TraceSummary {
  trace_id: string;
  span_name: string;
  service_name: string;
  duration_ms?: number;
  status_code: SpanStatusCode;
  span_count?: number;
  start_time?: number;
  end_time?: number;
  notice?: boolean;
  message?: string;
  root_span_id?: string;
  error_count?: number;
  warning_count?: number;
}

export type ViewMode = 'overview' | 'explore' | 'timeline' | 'metrics' | 'agents';

export interface TimelineSpan extends Span {
  level: number;
  children: TimelineSpan[];
  depth: number;
  x: number;
  y: number;
  width: number;
  height: number;
}
