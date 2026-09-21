import type { InsightFilters } from './insights-types';
import type { ViewMode } from './state';

export type TimePreset = '1h' | '6h' | '24h' | '7d' | 'custom';

export interface ExploreQueryOptions {
  limit?: number;
}

export interface InvestigationQueryOptions {
  traceIds: string[];
}

export interface HashState {
  view?: ViewMode;
  traceId?: string;
}

const PRESET_SECONDS: Record<Exclude<TimePreset, 'custom'>, number> = {
  '1h': 60 * 60,
  '6h': 6 * 60 * 60,
  '24h': 24 * 60 * 60,
  '7d': 7 * 24 * 60 * 60
};

function setOptionalNumber(params: URLSearchParams, key: string, value: number | undefined) {
  if (typeof value === 'number' && Number.isFinite(value)) {
    params.set(key, String(value));
  }
}

function setOptionalTrimmedString(params: URLSearchParams, key: string, value: string | undefined) {
  if (typeof value === 'string' && value.trim().length > 0) {
    params.set(key, value.trim());
  }
}

export function normalizeFilters(filters: InsightFilters): InsightFilters {
  const normalized: InsightFilters = {
    from: Math.floor(filters.from),
    to: Math.floor(filters.to),
    scope: filters.scope ?? 'requests'
  };

  if (filters.service?.trim()) normalized.service = filters.service.trim();
  if (filters.operation?.trim()) normalized.operation = filters.operation.trim();
  if (filters.status === 'error') normalized.status = 'error';
  if (typeof filters.min_duration_ms === 'number' && Number.isFinite(filters.min_duration_ms)) {
    normalized.min_duration_ms = filters.min_duration_ms;
  }
  if (typeof filters.max_duration_ms === 'number' && Number.isFinite(filters.max_duration_ms)) {
    normalized.max_duration_ms = filters.max_duration_ms;
  }

  const attribute = filters.attribute?.trim();
  if (attribute) {
    normalized.attribute = attribute;
    normalized.value = filters.value ?? '';
  }

  return normalized;
}

export function buildFilterParams(filters: InsightFilters): URLSearchParams {
  const normalized = normalizeFilters(filters);
  const params = new URLSearchParams({
    from: String(normalized.from),
    to: String(normalized.to),
    scope: normalized.scope ?? 'requests'
  });

  setOptionalTrimmedString(params, 'service', normalized.service);
  setOptionalTrimmedString(params, 'operation', normalized.operation);
  if (normalized.status === 'error') {
    params.set('status', 'error');
  }
  setOptionalNumber(params, 'min_duration_ms', normalized.min_duration_ms);
  setOptionalNumber(params, 'max_duration_ms', normalized.max_duration_ms);
  if (normalized.attribute !== undefined) {
    params.set('attribute', normalized.attribute);
    params.set('value', normalized.value ?? '');
  }

  return params;
}

export function buildInsightsUrl(filters: InsightFilters): string {
  return `/api/insights?${buildFilterParams(filters).toString()}`;
}

export function buildExploreUrl(filters: InsightFilters, options: ExploreQueryOptions = {}): string {
  const params = buildFilterParams(filters);
  if (typeof options.limit === 'number' && Number.isFinite(options.limit)) {
    params.set('limit', String(options.limit));
  }
  return `/api/explore?${params.toString()}`;
}

export function buildInvestigationUrl(filters: InsightFilters, options: InvestigationQueryOptions): string {
  const params = buildFilterParams(filters);
  for (const traceId of options.traceIds) {
    if (traceId.trim()) {
      params.append('trace_id', traceId.trim());
    }
  }
  return `/api/investigation?${params.toString()}`;
}

export function getPresetRange(preset: Exclude<TimePreset, 'custom'>, nowSeconds = Math.floor(Date.now() / 1000)): InsightFilters {
  // Include spans in the current second, matching the native API's default.
  const to = Math.floor(nowSeconds) + 1;
  return {
    from: to - PRESET_SECONDS[preset],
    to,
    scope: 'requests'
  };
}

export function detectPreset(filters: InsightFilters, nowSeconds = Math.floor(Date.now() / 1000)): TimePreset {
  const tolerance = 90;
  for (const preset of Object.keys(PRESET_SECONDS) as Array<Exclude<TimePreset, 'custom'>>) {
    const seconds = PRESET_SECONDS[preset];
    const expectedFrom = nowSeconds - seconds;
    if (Math.abs(filters.to - nowSeconds) <= tolerance && Math.abs(filters.from - expectedFrom) <= tolerance) {
      return preset;
    }
  }
  return 'custom';
}

export function toDatetimeLocalValue(unixSeconds: number): string {
  const date = new Date(unixSeconds * 1000);
  const pad = (value: number) => String(value).padStart(2, '0');
  return [
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`,
    `${pad(date.getHours())}:${pad(date.getMinutes())}`
  ].join('T');
}

export function fromDatetimeLocalValue(value: string): number | null {
  if (!value) {
    return null;
  }
  const parsed = new Date(value);
  const millis = parsed.getTime();
  if (!Number.isFinite(millis)) {
    return null;
  }
  return Math.floor(millis / 1000);
}

export function parseHash(hash: string): HashState {
  const trimmed = hash.startsWith('#') ? hash.slice(1) : hash;
  const params = new URLSearchParams(trimmed);
  const view = params.get('view') as ViewMode | null;
  const traceId = params.get('trace') ?? undefined;
  return {
    view: view ?? undefined,
    traceId
  };
}

export function buildHash(state: HashState): string {
  const params = new URLSearchParams();
  if (state.view) {
    params.set('view', state.view);
  }
  if (state.traceId) {
    params.set('trace', state.traceId);
  }
  const serialized = params.toString();
  return serialized ? `#${serialized}` : '#';
}
