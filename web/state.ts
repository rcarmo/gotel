// Application state management
export interface Span {
  id?: number;
  trace_id: string;
  span_id: string;
  parent_span_id?: string | null;
  service_name: string;
  span_name: string;
  start_time: number; // Unix timestamp in nanoseconds
  end_time: number; // Unix timestamp in nanoseconds
  duration_ms?: number; // Duration in milliseconds
  status_code: SpanStatusCode;
  status_message?: string;
  attributes?: Record<string, unknown>;
  events?: SpanEvent[];
  links?: SpanLink[];
  data?: Record<string, unknown>;
}

export interface SpanEvent {
  name: string;
  timestamp: number; // Unix timestamp in nanoseconds
  attributes?: Record<string, unknown>;
}

export interface SpanLink {
  trace_id: string;
  span_id: string;
  attributes?: Record<string, unknown>;
}

export type SpanStatusCode = 0 | 1 | 2 | number; // 0=Unset, 1=OK, 2=ERROR

export interface Exception {
  trace_id: string;
  span_id: string;
  service_name: string;
  span_name: string;
  exception_type?: string;
  message?: string;
  stack_trace?: string;
  timestamp: number; // Unix timestamp in nanoseconds
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
  start_time?: number; // Unix timestamp in nanoseconds
  end_time?: number; // Unix timestamp in nanoseconds
  notice?: boolean;
  message?: string;
  root_span_id?: string;
  error_count?: number;
  warning_count?: number;
}

export type ViewMode = 'traces' | 'timeline' | 'exceptions' | 'metrics';

export interface MetricsData {
  total_traces: number;
  total_spans: number;
  error_spans: number;
  warning_spans: number;
  total_exceptions: number;
  avg_duration_ms: number;
  p50_duration_ms: number;
  p95_duration_ms: number;
  p99_duration_ms: number;
  services_count: number;
  operations_count: number;
  error_rate: number;
}

export interface TimelineSpan extends Span {
  level: number;
  children: TimelineSpan[];
  depth: number;
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface ServiceMetrics {
  service_name: string;
  span_count: number;
  error_count: number;
  warning_count: number;
  avg_duration_ms: number;
  p95_duration_ms: number;
}

// Global state
let currentTraces: TraceSummary[] = [];
let currentSpans: Span[] = [];
let currentExceptions: Exception[] = [];
let loading = false;
let selectedTraceId: string | null = null;
let viewMode: ViewMode = 'traces';

// State getters
export const getState = () => ({
  currentTraces,
  currentSpans,
  currentExceptions,
  loading,
  selectedTraceId,
  viewMode
});

// State setters
export const setCurrentTraces = (traces: TraceSummary[]) => {
  currentTraces = traces;
  if (traces.length > 0 && !selectedTraceId) {
    selectedTraceId = traces[0].trace_id;
  }
};

export const setCurrentSpans = (spans: Span[]) => {
  currentSpans = spans;
};

export const setCurrentExceptions = (exceptions: Exception[]) => {
  currentExceptions = exceptions;
};

export const setLoading = (isLoading: boolean) => {
  loading = isLoading;
};

export const setSelectedTraceId = (traceId: string | null) => {
  selectedTraceId = traceId;
};

export const setViewMode = (newViewMode: ViewMode) => {
  viewMode = newViewMode;
};