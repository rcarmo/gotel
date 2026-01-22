import { h } from 'preact';
import { html } from 'htm/preact';
import type { TraceSummary, Exception, ViewMode } from '../state';
import { calculatePercentile } from '../utils';

interface StatsDashboardProps {
  traces: TraceSummary[];
  spans: any[];
  exceptions: Exception[];
}

// Statistics dashboard component - Azure Portal KPI style
export function StatsDashboard({ traces, spans, exceptions }: StatsDashboardProps) {
  // Pre-calculate metrics for performance optimization
  const errorSpansCount = spans.filter(s => s.status_code === 2).length;
  const errorRate = spans.length > 0 ? (errorSpansCount / spans.length) * 100 : 0;
  const uniqueServicesCount = new Set(spans.map(s => s.service_name)).size;
  const uniqueOperationsCount = new Set(spans.map(s => s.span_name)).size;
  const avgDuration = spans.length > 0 
    ? (spans.reduce((sum, s) => sum + (s.duration_ms || 0), 0) / spans.length).toFixed(1) 
    : '0';
  
  return html`
    <div class="fluent-grid fluent-grid--4" style="margin-bottom: var(--space-xl);">
      <!-- Total Traces KPI -->
      <div class="fluent-metric-tile fluent-metric-tile--accent-brand">
        ${traces.length > 0 && traces[0]?.notice ? html`
          <div class="gotel-metric-card__notice-badge" title="${traces[0]?.message || 'System Notice'}">!</div>
        ` : ''}
        <div class="fluent-metric-tile__label">Total Traces</div>
        <div class="fluent-metric-tile__value">${traces.length}</div>
        <div class="fluent-body2">${traces[0]?.notice ? 'System Notice' : 'Distributed traces collected'}</div>
      </div>
      
      <!-- Total Spans KPI -->
      <div class="fluent-metric-tile fluent-metric-tile--accent-success">
        <div class="fluent-metric-tile__label">Total Spans</div>
        <div class="fluent-metric-tile__value">${spans.length}</div>
        <div class="fluent-body2">Individual operations tracked</div>
      </div>
      
      <!-- Error Spans KPI -->
      <div class="fluent-metric-tile fluent-metric-tile--accent-error">
        <div class="fluent-metric-tile__label">Error Spans</div>
        <div class="fluent-metric-tile__value">${errorSpansCount}</div>
        <div class="fluent-body2" style="color: ${errorRate > 5 ? 'var(--fluent-error)' : 'inherit'}">
          ${errorRate.toFixed(1)}% error rate
        </div>
      </div>
      
      <!-- Exceptions KPI -->
      <div class="fluent-metric-tile fluent-metric-tile--accent-warning">
        <div class="fluent-metric-tile__label">Exceptions</div>
        <div class="fluent-metric-tile__value">${exceptions.length}</div>
        <div class="fluent-body2">Exception events recorded</div>
      </div>
    </div>
    
    <!-- Secondary Metrics Row -->
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