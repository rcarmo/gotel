import { describe, expect, test } from 'bun:test';
import { normalizeSpan } from './state';

describe('stored span contract', () => {
  test('maps backend timestamps and status into UI fields', () => {
    const span = normalizeSpan({
      trace_id: 'trace', span_id: 'span', service_name: 'test', span_name: 'operation',
      start_time_unix_nano: 1790012345000000000,
      end_time_unix_nano: 1790012345005000000,
      duration_ms: 5,
      status: { code: 2, message: 'failed' },
      events: [{ name: 'exception', timestamp: 1790012345000000000 }]
    });
    expect(span.start_time).toBe(1790012345000000000);
    expect(span.end_time).toBe(1790012345005000000);
    expect(span.status_code).toBe(2);
    expect(span.status_message).toBe('failed');
    expect(span.duration_ms).toBe(5);
    expect(span.events).toHaveLength(1);
  });

  test('keeps normalised fields when reading an existing UI fixture', () => {
    expect(normalizeSpan({ start_time: 10, end_time: 20, status_code: 1 }).start_time).toBe(10);
    expect(normalizeSpan({ start_time: 10, end_time: 20, status_code: 1 }).status_code).toBe(1);
  });
});
