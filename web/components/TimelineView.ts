import { h } from 'preact';
import { html } from 'htm/preact';
import type { Span } from '../state';

// Import PerfCascade for waterfall visualization
declare const perfCascade: any;

interface TimelineViewProps {
  selectedTraceId: string | null;
  spans: Span[];
}

// Helper function to convert OTel spans to HAR-like format for PerfCascade
function convertSpansToHarFormat(spans: Span[]): any {
  if (spans.length === 0) return null;
  
  // Sort spans by start time
  const sortedSpans = [...spans].sort((a, b) => a.start_time - b.start_time);
  const firstSpan = sortedSpans[0];
  if (!firstSpan) return null;
  
  // Calculate the earliest start time as the page start reference
  const pageStartTime = new Date(firstSpan.start_time / 1000000);
  
  // Create HAR-like structure
  const harData = {
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
        const startTime = new Date(span.start_time / 1000000);
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
            status: span.status_code === 2 ? 500 : span.status_code === 1 ? 400 : 200,
            statusText: span.status_code === 2 ? 'Error' : span.status_code === 1 ? 'Warning' : 'OK',
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
          // Store custom data for potential use
          _spanId: span.span_id,
          _parentSpanId: span.parent_span_id,
          _serviceName: span.service_name,
          _spanName: span.span_name,
          _statusCode: span.status_code
        };
      })
    }
  };
  
  return harData;
}

// Enhanced timeline visualization for OpenTelemetry spans - Azure Monitor style
export function TimelineView({ selectedTraceId, spans }: TimelineViewProps) {
  if (!selectedTraceId || spans.length === 0) {
    return html`
      <div class="fluent-empty-state">
        <div class="fluent-empty-state__icon">⏱️</div>
        <div class="fluent-empty-state__title">No Timeline Data</div>
        <p class="fluent-empty-state__text">
          Select a trace from the Trace Explorer to view its timeline visualization.
        </p>
      </div>
    `;
  }
  
  // Filter spans for selected trace
  const traceSpans = spans.filter(span => span.trace_id === selectedTraceId);
  if (traceSpans.length === 0) {
    return html`
      <div class="fluent-alert fluent-alert--warning">
        <div class="fluent-alert__icon">⚠️</div>
        <div class="fluent-alert__content">
          <div class="fluent-alert__title">No Spans Found</div>
          <div class="fluent-alert__message">No spans found for the selected trace ID.</div>
        </div>
      </div>
    `;
  }
  
  // Convert OTel spans to HAR format for PerfCascade
  const harData = convertSpansToHarFormat(traceSpans);
  
  // Create container for PerfCascade visualization
  const containerId = `perfcascade-${selectedTraceId.slice(0, 8)}`;
  
  // Render PerfCascade after component mounts (only in browser)
  if (typeof window !== 'undefined' && typeof document !== 'undefined') {
    // Use requestAnimationFrame for more reliable DOM timing
    requestAnimationFrame(() => {
      try {
        if (typeof perfCascade !== 'undefined' && harData) {
          const container = document.getElementById(containerId);
          if (container && container.children.length === 0) {
            // Create PerfCascade visualization with options
            const perfCascadeSvg = perfCascade.fromHar(harData, {
              rowHeight: 23,
              showAlignmentHelpers: true,
              showIndicatorIcons: true,
              leftColumnWidth: 250
            });
            container.appendChild(perfCascadeSvg);
          }
        } else {
          // Show fallback message if PerfCascade not available
          const container = document.getElementById(containerId);
          if (container && container.children.length === 0) {
            container.innerHTML = '<p class="fluent-body2" style="padding: var(--space-l); color: var(--color-text-muted);">PerfCascade library not loaded. Showing basic span list below.</p>';
          }
        }
      } catch (error) {
        console.error('Error rendering PerfCascade:', error);
        const container = document.getElementById(containerId);
        if (container) {
          container.innerHTML = `<div class="fluent-alert fluent-alert--error" style="margin: var(--space-l);"><div class="fluent-alert__content"><div class="fluent-alert__title">Rendering Error</div><div class="fluent-alert__message">${(error as Error).message}</div></div></div>`;
        }
      }
    });
  }
  
  // Calculate metrics
  const totalDuration = traceSpans.reduce((sum, s) => sum + (s.duration_ms || 0), 0);
  const errorCount = traceSpans.filter(s => s.status_code === 2).length;
  const serviceCount = new Set(traceSpans.map(s => s.service_name)).size;
  
  return html`
    <div class="gotel-timeline-container">
      <!-- Trace Summary Header -->
      <div class="fluent-grid fluent-grid--4" style="margin-bottom: var(--space-l);">
        <div class="gotel-stat-card">
          <div class="gotel-stat-card__label">Trace ID</div>
          <div style="font-family: var(--font-family-mono); font-size: var(--font-size-200); word-break: break-all;">
            ${selectedTraceId}
          </div>
        </div>
        <div class="gotel-stat-card">
          <div class="gotel-stat-card__value">${traceSpans.length}</div>
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
      
      <!-- Legend -->
      <div class="gotel-timeline-legend">
        <div class="fluent-subtitle2">Legend</div>
        <div class="gotel-timeline-legend-items">
          <span class="gotel-timeline-legend-item">
            <span class="fluent-badge fluent-badge--tint-success">🟢</span>
            <span>Success (OK)</span>
          </span>
          <span class="gotel-timeline-legend-item">
            <span class="fluent-badge fluent-badge--tint-warning">🟡</span>
            <span>Warning</span>
          </span>
          <span class="gotel-timeline-legend-item">
            <span class="fluent-badge fluent-badge--tint-error">🔴</span>
            <span>Error</span>
          </span>
        </div>
      </div>
      
      <!-- PerfCascade container -->
      <div id="${containerId}" class="perfcascade-container" style="min-height: 200px; overflow-x: auto;"></div>
      
      <!-- Fallback span list -->
      <details class="gotel-margin-top-4">
        <summary>Span Details (${traceSpans.length} spans)</summary>
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
              ${traceSpans.map(span => html`
                <tr>
                  <td><code>${span.service_name}</code></td>
                  <td>${span.span_name}</td>
                  <td style="text-align: right;">${(span.duration_ms || 0).toFixed(2)}ms</td>
                  <td>
                    <span class="fluent-badge ${span.status_code === 2 ? 'fluent-badge--tint-error' : span.status_code === 1 ? 'fluent-badge--tint-warning' : 'fluent-badge--tint-success'}">
                      ${span.status_code === 2 ? 'Error' : span.status_code === 1 ? 'Warning' : 'OK'}
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