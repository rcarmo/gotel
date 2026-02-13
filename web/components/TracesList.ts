import { h } from 'preact';
import { html } from 'htm/preact';
import type { TraceSummary, ViewMode } from '../state';
import { formatDuration, getStatusColor, getStatusText } from '../utils';

interface TracesListProps {
  traces: TraceSummary[];
  spans: any[];
  selectedTraceId: string | null;
  onTraceSelect: (traceId: string) => void;
  onViewModeChange: (mode: ViewMode) => void;
  currentViewMode: ViewMode;
}

// Render traces list with service grouping - Azure Portal Resource List style
export function TracesList({ 
  traces, 
  spans, 
  selectedTraceId, 
  onTraceSelect, 
  onViewModeChange,
  currentViewMode 
}: TracesListProps) {
  const tracesByService = new Map<string, TraceSummary[]>();
  traces.forEach(trace => {
    if (!tracesByService.has(trace.service_name)) {
      tracesByService.set(trace.service_name, []);
    }
    tracesByService.get(trace.service_name)!.push(trace);
  });
  
  if (traces.length === 0) {
    return html`
      <div class="fluent-empty-state">
        <div class="fluent-empty-state__icon">📭</div>
        <div class="fluent-empty-state__title">No Traces Found</div>
        <p class="fluent-empty-state__text">
          Start your application with OpenTelemetry instrumentation to see traces here.
        </p>
        <button class="fluent-btn fluent-btn--primary">Learn More</button>
      </div>
    `;
  }
  
  return html`
    <div class="fluent-grid fluent-grid--2">
      <!-- Traces List Panel -->
      <div class="fluent-card" style="margin: 0;">
        <div class="fluent-card__header">
          <h3 class="fluent-card__title">Traces by Service</h3>
          <span class="fluent-badge fluent-badge--tint-info">${traces.length} traces</span>
        </div>
        <div class="fluent-card__body gotel-scrollable-y" style="padding: 0; max-height: 500px;">
          ${Array.from(tracesByService.entries()).map(([service, serviceTraces]) => html`
            <div class="fluent-list" style="border: none; border-radius: 0; border-bottom: 1px solid var(--color-border);">
              <div class="fluent-list__header">${service} (${serviceTraces.length})</div>
              ${serviceTraces.map(trace => html`
                <div 
                  class="fluent-list__item ${selectedTraceId === trace.trace_id ? 'fluent-list__item--selected' : ''}"
                  onClick=${() => {
                    onTraceSelect(trace.trace_id);
                    onViewModeChange('timeline');
                  }}
                  role="button"
                  tabindex="0"
                  onKeyDown=${(e: KeyboardEvent) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      onTraceSelect(trace.trace_id);
                      onViewModeChange('timeline');
                    }
                  }}
                >
                  <div class="fluent-list__item-icon">
                    ${trace.status_code === 2 ? '🔴' : trace.status_code === 1 ? '🟢' : '⚪'}
                  </div>
                  <div class="fluent-list__item-content">
                    <div class="fluent-list__item-title">${trace.span_name || 'Root Span'}</div>
                    <div class="fluent-list__item-meta">
                      ID: ${trace.trace_id.slice(0, 8)}... · ${formatDuration(trace.duration_ms || 0)}
                    </div>
                  </div>
                  ${trace.status_code === 2 ? html`
                    <span class="fluent-badge fluent-badge--tint-error">Error</span>
                  ` : ''}
                </div>
              `)}
            </div>
          `)}
        </div>
      </div>
      
      <!-- Selected Trace Details Panel -->
      <div class="fluent-card" style="margin: 0;">
        <div class="fluent-card__header">
          <h3 class="fluent-card__title">Trace Details</h3>
        </div>
        <div class="fluent-card__body">
          ${selectedTraceId ? html`
            <div class="fluent-body1" style="margin-bottom: var(--space-l);">
              <strong>Trace ID:</strong>
              <code style="margin-left: var(--space-s);">${selectedTraceId}</code>
            </div>
            <p class="fluent-body2">
              Select "Timeline" from the navigation to view the detailed span visualization for this trace.
            </p>
            <div style="margin-top: var(--space-l);">
              <button 
                class="fluent-btn fluent-btn--primary"
                onClick=${() => onViewModeChange('timeline')}
              >
                View Timeline
              </button>
            </div>
          ` : html`
            <div class="fluent-empty-state" style="padding: var(--space-xl);">
              <div class="fluent-empty-state__icon">👆</div>
              <div class="fluent-empty-state__title">Select a Trace</div>
              <p class="fluent-empty-state__text">
                Click on a trace from the list to view its details and timeline visualization.
              </p>
            </div>
          `}
        </div>
      </div>
    </div>
  `;
}