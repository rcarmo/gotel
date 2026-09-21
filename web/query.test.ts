import { describe, expect, it } from 'bun:test';
import {
  buildExploreUrl,
  buildFilterParams,
  buildHash,
  buildInsightsUrl,
  buildInvestigationUrl,
  normalizeFilters,
  parseHash
} from './query';

describe('query serialization', () => {
  it('serializes shared filters consistently', () => {
    const filters = normalizeFilters({
      from: 100.9,
      to: 200.4,
      scope: 'all',
      service: ' api ',
      operation: ' GET /items ',
      status: 'error',
      min_duration_ms: 50,
      max_duration_ms: 200,
      attribute: ' http.method ',
      value: ' GET '
    });

    const params = buildFilterParams(filters);
    expect(params.toString()).toBe('from=100&to=200&scope=all&service=api&operation=GET+%2Fitems&status=error&min_duration_ms=50&max_duration_ms=200&attribute=http.method&value=+GET+');
    expect(buildInsightsUrl(filters)).toContain('/api/insights?');
    expect(buildExploreUrl(filters, { limit: 25 })).toContain('limit=25');
    expect(buildInvestigationUrl(filters, { traceIds: ['trace-a', ' trace-b '] })).toContain('trace_id=trace-a');
    expect(buildInvestigationUrl(filters, { traceIds: ['trace-a', ' trace-b '] })).toContain('trace_id=trace-b');
  });

  it('round-trips helpful trace deep links', () => {
    const hash = buildHash({ view: 'timeline', traceId: 'trace-123' });
    expect(hash).toBe('#view=timeline&trace=trace-123');
    expect(parseHash(hash)).toEqual({ view: 'timeline', traceId: 'trace-123' });
  });
});

it('preserves empty and whitespace attribute values', () => {
  for (const value of ['', '  exact  ']) {
    const params = buildFilterParams({from:1,to:2,attribute:'key',value});
    expect(params.has('value')).toBe(true);
    expect(params.get('value')).toBe(value);
  }
});
