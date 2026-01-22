import { h } from 'preact';
import { html } from 'htm/preact';
import type { Span, ViewMode } from '../state';
import { calculatePercentile } from '../utils';

interface MetricsViewProps {
  spans: Span[];
  viewMode: ViewMode;
}

// Render metrics view - Azure Monitor Style
export function MetricsView({ spans, viewMode }: MetricsViewProps) {
  if (spans.length === 0) {
    return html`
      <div class="fluent-empty-state">
        <div class="fluent-empty-state__icon">📈</div>
        <div class="fluent-empty-state__title">No Metrics Available</div>
        <p class="fluent-empty-state__text">
          Start collecting traces to see performance metrics here.
        </p>
      </div>
    `;
  }

  // Calculate metrics
  const services = [...new Set(spans.map(s => s.service_name))];
  const operations = [...new Set(spans.map(s => s.span_name))];
  
  // Service performance metrics
  const serviceMetrics = services.map(service => {
    const serviceSpans = spans.filter(s => s.service_name === service);
    const errorCount = serviceSpans.filter(s => s.status_code === 2).length;
    const avgDuration = serviceSpans.reduce((sum, s) => sum + (s.duration_ms || 0), 0) / serviceSpans.length;
    
    return {
      service,
      totalSpans: serviceSpans.length,
      errorCount,
      errorRate: (errorCount / serviceSpans.length) * 100,
      avgDuration: avgDuration.toFixed(2),
      p95Duration: (calculatePercentile(serviceSpans.map(s => s.duration_ms || 0), 95) || 0).toFixed(2)
    };
  });
  
  // Operation frequency
  const operationFreq = operations.map(op => {
    const opSpans = spans.filter(s => s.span_name === op);
    return {
      operation: op,
      count: opSpans.length,
      avgDuration: (opSpans.reduce((sum, s) => sum + (s.duration_ms || 0), 0) / opSpans.length).toFixed(2)
    };
  }).sort((a, b) => b.count - a.count).slice(0, 10);

  return html`
    <div>
      <!-- Service Performance Section -->
      <div class="fluent-card" style="margin-bottom: var(--space-xl);">
        <div class="fluent-card__header">
          <h3 class="fluent-card__title">Service Performance</h3>
          <span class="fluent-badge fluent-badge--tint-info">${services.length} services</span>
        </div>
        <div class="fluent-card__body" style="padding: 0;">
          <div class="fluent-table-container" style="border: none;">
            <table class="fluent-table">
              <thead>
                <tr>
                  <th>Service</th>
                  <th style="text-align: right;">Spans</th>
                  <th style="text-align: right;">Error Rate</th>
                  <th style="text-align: right;">Avg Duration</th>
                  <th style="text-align: right;">P95 Duration</th>
                  <th>Health</th>
                </tr>
              </thead>
              <tbody>
                ${serviceMetrics.map(metric => html`
                  <tr>
                    <td>
                      <div style="display: flex; align-items: center; gap: var(--space-s);">
                        <span style="width: 8px; height: 8px; border-radius: 50%; background: ${metric.errorRate > 5 ? 'var(--fluent-error)' : metric.errorRate > 1 ? 'var(--fluent-warning-icon)' : 'var(--fluent-success)'}"></span>
                        <code>${metric.service}</code>
                      </div>
                    </td>
                    <td style="text-align: right;">${metric.totalSpans}</td>
                    <td style="text-align: right;">
                      <span style="color: ${metric.errorRate > 5 ? 'var(--fluent-error)' : metric.errorRate > 1 ? 'var(--fluent-gray-160)' : 'var(--fluent-success)'}">
                        ${metric.errorRate.toFixed(1)}%
                      </span>
                    </td>
                    <td style="text-align: right;">${metric.avgDuration}ms</td>
                    <td style="text-align: right;">${metric.p95Duration}ms</td>
                    <td>
                      <span class="fluent-badge ${metric.errorRate > 5 ? 'fluent-badge--tint-error' : metric.errorRate > 1 ? 'fluent-badge--tint-warning' : 'fluent-badge--tint-success'}">
                        ${metric.errorRate > 5 ? 'Critical' : metric.errorRate > 1 ? 'Warning' : 'Healthy'}
                      </span>
                    </td>
                  </tr>
                `)}
              </tbody>
            </table>
          </div>
        </div>
      </div>
      
      <!-- Top Operations Section -->
      <div class="fluent-card" style="margin-bottom: var(--space-xl);">
        <div class="fluent-card__header">
          <h3 class="fluent-card__title">Top Operations</h3>
          <span class="fluent-caption1">By frequency</span>
        </div>
        <div class="fluent-card__body">
          <div class="fluent-grid fluent-grid--auto">
            ${operationFreq.map((op, index) => html`
              <div class="fluent-metric-tile fluent-metric-tile--clickable" style="text-align: left;">
                <div style="display: flex; justify-content: space-between; align-items: flex-start;">
                  <div class="fluent-caption1" style="color: var(--fluent-brand);">#${index + 1}</div>
                  <span class="fluent-badge fluent-badge--filled-info">${op.count}</span>
                </div>
                <div class="fluent-subtitle2" style="margin-top: var(--space-s); word-break: break-word;">${op.operation}</div>
                <div class="fluent-caption1">${op.avgDuration}ms avg</div>
              </div>
            `)}
          </div>
        </div>
      </div>
      
      <!-- Duration Distribution Section -->
      <div class="fluent-card">
        <div class="fluent-card__header">
          <h3 class="fluent-card__title">Duration Distribution</h3>
        </div>
        <div class="fluent-card__body">
          <div class="fluent-grid fluent-grid--4">
            ${[
              { label: '< 10ms', filter: (s: Span) => (s.duration_ms || 0) < 10, variant: 'success' },
              { label: '10-100ms', filter: (s: Span) => (s.duration_ms || 0) >= 10 && (s.duration_ms || 0) < 100, variant: 'warning' },
              { label: '100ms-1s', filter: (s: Span) => (s.duration_ms || 0) >= 100 && (s.duration_ms || 0) < 1000, variant: 'warning' },
              { label: '> 1s', filter: (s: Span) => (s.duration_ms || 0) >= 1000, variant: 'error' }
            ].map(bucket => {
              const count = spans.filter(bucket.filter).length;
              const percentage = ((count / spans.length) * 100).toFixed(1);
              return html`
                <div class="gotel-stat-card gotel-stat-card--${bucket.variant}">
                  <div class="gotel-stat-card__value">${count}</div>
                  <div class="gotel-stat-card__label">${bucket.label}</div>
                  <div class="gotel-stat-card__subtext">${percentage}%</div>
                </div>
              `;
            })}
          </div>
        </div>
      </div>
    </div>
  `;
}