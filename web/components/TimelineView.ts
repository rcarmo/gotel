import { h } from 'preact';
import { useEffect, useRef } from 'preact/hooks';
import { html } from 'htm/preact';
import type { Span } from '../state';

declare const perfCascade: any;

interface TimelineViewProps {
  selectedTraceId: string | null;
  spans: Span[];
  loading?: boolean;
  errorMessage?: string | null;
  onExportTrace?: (traceId: string) => void;
}

function convertSpansToHarFormat(spans: Span[]): any {
  if (spans.length === 0) return null;

  const sortedSpans = [...spans].sort((a, b) => a.start_time - b.start_time);
  const firstSpan = sortedSpans[0];
  if (!firstSpan) return null;

  const pageStartTime = new Date(firstSpan.start_time / 1_000_000);

  return {
    log: {
      version: '1.2',
      creator: {
        name: 'GoTel',
        version: '1.0'
      },
      pages: [{
        id: 'trace',
        startedDateTime: pageStartTime.toISOString(),
        title: 'OpenTelemetry Trace',
        pageTimings: {
          onContentLoad: -1,
          onLoad: -1
        }
      }],
      entries: sortedSpans.map((span) => {
        const startTime = new Date(span.start_time / 1_000_000);
        const duration = span.duration_ms || 0;

        return {
          pageref: 'trace',
          startedDateTime: startTime.toISOString(),
          time: duration,
          request: {
            method: 'SPAN',
            url: `${span.service_name}: ${span.span_name}`,
            httpVersion: 'HTTP/1.1',
            headers: [],
            cookies: [],
            queryString: [],
            headersSize: -1,
            bodySize: -1
          },
          response: {
            status: span.status_code === 2 ? 500 : span.status_code === 1 ? 200 : 204,
            statusText: span.status_code === 2 ? 'Error' : span.status_code === 1 ? 'OK' : 'Unset',
            httpVersion: 'HTTP/1.1',
            headers: [],
            cookies: [],
            content: {
              size: 0,
              mimeType: 'application/octet-stream'
            },
            redirectURL: '',
            headersSize: -1,
            bodySize: -1,
            _transferSize: 0
          },
          cache: {},
          timings: {
            blocked: -1,
            dns: -1,
            connect: -1,
            send: 0,
            wait: duration,
            receive: 0,
            ssl: -1
          },
          _spanId: span.span_id,
          _parentSpanId: span.parent_span_id,
          _serviceName: span.service_name,
          _spanName: span.span_name,
          _statusCode: span.status_code
        };
      })
    }
  };
}

function Waterfall({ selectedTraceId, spans }: { selectedTraceId: string; spans: Span[] }) {
  const container = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const target = container.current;
    if (!target) return;
    target.replaceChildren();
    try {
      if (typeof perfCascade === 'undefined') {
        target.textContent = 'PerfCascade library not loaded. Showing basic span list below.';
      } else {
        target.appendChild(perfCascade.fromHar(convertSpansToHarFormat(spans), {
          rowHeight: 23, showAlignmentHelpers: true, showIndicatorIcons: true, leftColumnWidth: 30
        }));
      }
    } catch (error) {
      target.textContent = 'Rendering error: ' + (error as Error).message;
    }
    return () => target.replaceChildren();
  }, [selectedTraceId, spans]);
  return html`<div ref=${container} class="perfcascade-container" style="min-height: 200px; overflow-x: auto;"></div>`;
}

export function TimelineView({ selectedTraceId, spans, loading = false, errorMessage = null, onExportTrace }: TimelineViewProps) {
  if (!selectedTraceId) {
    return html`
      <div class="fluent-empty-state">
        <div class="fluent-empty-state__icon">⏱️</div>
        <div class="fluent-empty-state__title">No Trace Selected</div>
        <p class="fluent-empty-state__text">
          Select a trace from the Trace Explorer to view its timeline visualization.
        </p>
      </div>
    `;
  }

  if (loading) {
    return html`
      <div class="fluent-empty-state">
        <div class="fluent-empty-state__icon">⏳</div>
        <div class="fluent-empty-state__title">Loading Trace Timeline</div>
        <p class="fluent-empty-state__text">
          Fetching all spans for the selected trace.
        </p>
      </div>
    `;
  }

  if (errorMessage) {
    return html`
      <div class="fluent-alert fluent-alert--error">
        <div class="fluent-alert__icon">⚠️</div>
        <div class="fluent-alert__content">
          <div class="fluent-alert__title">Unable to Load Trace Timeline</div>
          <div class="fluent-alert__message">${errorMessage}</div>
        </div>
      </div>
    `;
  }

  if (spans.length === 0) {
    return html`
      <div class="fluent-alert fluent-alert--warning">
        <div class="fluent-alert__icon">⚠️</div>
        <div class="fluent-alert__content">
          <div class="fluent-alert__title">No Spans Found</div>
          <div class="fluent-alert__message">The selected trace has no spans to display.</div>
        </div>
      </div>
    `;
  }

  const firstStart = spans.reduce((min, span) => Math.min(min, span.start_time), Infinity);
  const lastEnd = spans.reduce((max, span) => Math.max(max, span.end_time), -Infinity);
  const totalDuration = Math.max(0, (lastEnd - firstStart) / 1_000_000);
  const errorCount = spans.filter((span) => span.status_code === 2).length;
  const serviceCount = new Set(spans.map((span) => span.service_name)).size;

  return html`
    <div class="gotel-timeline-container">
      <div class="fluent-grid fluent-grid--4" style="margin-bottom: var(--space-l);">
        <div class="gotel-stat-card">
          <div class="gotel-stat-card__label">Trace ID</div>
          <div style="font-family: var(--font-family-mono); font-size: var(--font-size-200); word-break: break-all;">
            ${selectedTraceId}
          </div>
        </div>
        <div class="gotel-stat-card">
          <div class="gotel-stat-card__value">${spans.length}</div>
          <div class="gotel-stat-card__label">Total Spans</div>
        </div>
        <div class="gotel-stat-card">
          <div class="gotel-stat-card__value">${serviceCount}</div>
          <div class="gotel-stat-card__label">Services</div>
        </div>
        <div class="gotel-stat-card ${errorCount > 0 ? 'gotel-stat-card--error' : 'gotel-stat-card--success'}">
          <div class="gotel-stat-card__value">${errorCount}</div>
          <div class="gotel-stat-card__label">Errors</div>
        </div>
      </div>

      ${onExportTrace ? html`
        <div class="gotel-toolbar" style="margin-bottom: var(--space-l);">
          <div class="gotel-muted-text">Export this complete trace as an investigation bundle.</div>
          <button class="fluent-btn fluent-btn--primary" onClick=${() => onExportTrace(selectedTraceId)}>
            Export this trace
          </button>
        </div>
      ` : ''}

      <div class="gotel-timeline-legend">
        <div class="fluent-subtitle2">Legend</div>
        <div class="gotel-timeline-legend-items">
          <span class="gotel-timeline-legend-item">
            <span class="fluent-badge fluent-badge--tint-info">⚪</span>
            <span>Unset</span>
          </span>
          <span class="gotel-timeline-legend-item">
            <span class="fluent-badge fluent-badge--tint-success">🟢</span>
            <span>OK</span>
          </span>
          <span class="gotel-timeline-legend-item">
            <span class="fluent-badge fluent-badge--tint-error">🔴</span>
            <span>Error</span>
          </span>
        </div>
      </div>

      <${Waterfall} selectedTraceId=${selectedTraceId} spans=${spans} />

      <details class="gotel-margin-top-4" open>
        <summary>Span Details (${spans.length} spans, ${totalDuration.toFixed(2)}ms total)</summary>
        <div class="gotel-table-container gotel-margin-top-2">
          <table class="gotel-table fluent-table">
            <thead>
              <tr>
                <th>Service</th>
                <th>Span Name</th>
                <th>Duration</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              ${spans.map((span) => html`
                <tr>
                  <td><code>${span.service_name}</code></td>
                  <td>${span.span_name}</td>
                  <td style="text-align: right;">${(span.duration_ms || 0).toFixed(2)}ms</td>
                  <td>
                    <span class="fluent-badge ${span.status_code === 2 ? 'fluent-badge--tint-error' : span.status_code === 1 ? 'fluent-badge--tint-success' : 'fluent-badge--tint-info'}">
                      ${span.status_code === 2 ? 'Error' : span.status_code === 1 ? 'OK' : 'Unset'}
                    </span>
                  </td>
                </tr>
              `)}
            </tbody>
          </table>
        </div>
      </details>
    </div>
  `;
}
