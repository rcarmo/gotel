import { h } from 'preact';
import { html } from 'htm/preact';
import type { Exception, Span, TraceSummary } from '../state';

interface StatsDashboardProps {
  traces: TraceSummary[];
  spans: Span[];
  exceptions: Exception[];
}

// Statistics dashboard component - Azure Portal KPI style
export function StatsDashboard({ traces, spans, exceptions }: StatsDashboardProps) {
  const errorSpansCount = spans.filter((span) => span.status_code === 2).length;
  const errorRate = spans.length > 0 ? (errorSpansCount / spans.length) * 100 : 0;
  const uniqueServicesCount = new Set(spans.map((span) => span.service_name)).size;
  const uniqueOperationsCount = new Set(spans.map((span) => span.span_name)).size;
  const avgDuration = spans.length > 0
    ? (spans.reduce((sum, span) => sum + (span.duration_ms || 0), 0) / spans.length).toFixed(1)
    : '0';

  return html`
    <div class="fluent-grid fluent-grid--4" style="margin-bottom: var(--space-xl);">
      <div class="fluent-metric-tile fluent-metric-tile--accent-brand">
        <div class="fluent-metric-tile__label">Total Traces</div>
        <div class="fluent-metric-tile__value">${traces.length}</div>
        <div class="fluent-body2">Distributed traces collected</div>
      </div>

      <div class="fluent-metric-tile fluent-metric-tile--accent-success">
        <div class="fluent-metric-tile__label">Total Spans</div>
        <div class="fluent-metric-tile__value">${spans.length}</div>
        <div class="fluent-body2">Individual operations tracked</div>
      </div>

      <div class="fluent-metric-tile fluent-metric-tile--accent-error">
        <div class="fluent-metric-tile__label">Error Spans</div>
        <div class="fluent-metric-tile__value">${errorSpansCount}</div>
        <div class="fluent-body2" style="color: ${errorRate > 5 ? 'var(--fluent-error)' : 'inherit'}">
          ${errorRate.toFixed(1)}% error rate
        </div>
      </div>

      <div class="fluent-metric-tile fluent-metric-tile--accent-warning">
        <div class="fluent-metric-tile__label">Exceptions</div>
        <div class="fluent-metric-tile__value">${exceptions.length}</div>
        <div class="fluent-body2">Exception events recorded</div>
      </div>
    </div>

    <div class="fluent-grid fluent-grid--4" style="margin-bottom: var(--space-l);">
      <div class="gotel-stat-card">
        <div class="gotel-stat-card__value">${avgDuration}ms</div>
        <div class="gotel-stat-card__label">Avg Duration</div>
      </div>

      <div class="gotel-stat-card">
        <div class="gotel-stat-card__value">${uniqueServicesCount}</div>
        <div class="gotel-stat-card__label">Services</div>
      </div>

      <div class="gotel-stat-card">
        <div class="gotel-stat-card__value">${uniqueOperationsCount}</div>
        <div class="gotel-stat-card__label">Operations</div>
      </div>

      <div class="gotel-stat-card ${errorRate > 5 ? 'gotel-stat-card--error' : errorRate > 1 ? 'gotel-stat-card--warning' : 'gotel-stat-card--success'}">
        <div class="gotel-stat-card__value">${errorRate.toFixed(1)}%</div>
        <div class="gotel-stat-card__label">Error Rate</div>
      </div>
    </div>
  `;
}
