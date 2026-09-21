import { h } from 'preact';
import { html } from 'htm/preact';
import type { ExploreResult, ExploreTrace } from '../insights-types';
import { formatCount, formatDateTimeFromNanos, formatDuration, getStatusText } from '../utils';

interface ExploreViewProps {
  result: ExploreResult | null;
  loading?: boolean;
  errorMessage?: string | null;
  imported?: boolean;
  selectedTraceId: string | null;
  selectedTraceIds: Set<string>;
  onSelectTrace: (traceId: string) => void;
  onToggleTrace: (traceId: string, checked: boolean) => void;
  onExportSelected?: () => void;
  exportDisabled?: boolean;
}

function renderRows(traces: ExploreTrace[], imported: boolean, selectedTraceIds: Set<string>, selectedTraceId: string | null, onSelectTrace: (traceId: string) => void, onToggleTrace: (traceId: string, checked: boolean) => void) {
  return traces.map((trace) => html`
    <tr class=${selectedTraceId === trace.trace_id ? 'selected' : ''}>
      ${!imported ? html`
        <td>
          <input
            type="checkbox"
            aria-label=${`Select trace ${trace.trace_id}`}
            checked=${selectedTraceIds.has(trace.trace_id)}
            onInput=${(event: Event) => onToggleTrace(trace.trace_id, (event.currentTarget as HTMLInputElement).checked)}
          />
        </td>
      ` : ''}
      <td><code>${trace.service_name}</code></td>
      <td>${trace.span_name}</td>
      <td>${getStatusText(trace.status_code)}</td>
      <td style="text-align: right;">${formatCount(trace.span_count)}</td>
      <td style="text-align: right;">${formatDuration(trace.duration_ms)}</td>
      <td>${formatDateTimeFromNanos(trace.start_time)}</td>
      <td>
        <button class="fluent-btn fluent-btn--secondary fluent-btn--small" onClick=${() => onSelectTrace(trace.trace_id)}>
          Open timeline
        </button>
      </td>
    </tr>
  `);
}

export function ExploreView({ result, loading = false, errorMessage = null, imported = false, selectedTraceId, selectedTraceIds, onSelectTrace, onToggleTrace, onExportSelected, exportDisabled = false }: ExploreViewProps) {
  if (errorMessage) {
    return html`
      <div class="fluent-alert fluent-alert--error">
        <div class="fluent-alert__icon">⚠️</div>
        <div class="fluent-alert__content">
          <div class="fluent-alert__title">Unable to load trace search</div>
          <div class="fluent-alert__message">${errorMessage}</div>
        </div>
      </div>
    `;
  }

  if (loading && !result) {
    return html`
      <div class="fluent-empty-state">
        <div class="fluent-empty-state__icon">⏳</div>
        <div class="fluent-empty-state__title">Searching traces</div>
      </div>
    `;
  }

  if (!result) {
    return html`
      <div class="fluent-empty-state">
        <div class="fluent-empty-state__icon">🔎</div>
        <div class="fluent-empty-state__title">No trace search loaded</div>
        <p class="fluent-empty-state__text">Use the shared filters and open trace search.</p>
      </div>
    `;
  }

  return html`
    <div class="gotel-stack">
      ${imported ? html`
        <div class="fluent-alert fluent-alert--info">
          <div class="fluent-alert__icon">📦</div>
          <div class="fluent-alert__content">
            <div class="fluent-alert__title">Imported investigation bundle</div>
            <div class="fluent-alert__message">Trace search is showing every downloaded trace with at least one included span matching the current bucket or exported filters. No network requests are made while inspecting it.</div>
          </div>
        </div>
      ` : html`
        <div class="fluent-alert fluent-alert--warning">
          <div class="fluent-alert__icon">🔒</div>
          <div class="fluent-alert__content">
            <div class="fluent-alert__title">Investigation export</div>
            <div class="fluent-alert__message">Exported bundles omit prompts, responses and arbitrary attributes, but names and labels may still identify workloads. Select up to 20 traces.</div>
          </div>
        </div>
      `}

      <div class="gotel-toolbar">
        <div class="gotel-muted-text">
          ${formatCount(result.traces.length)} matching traces${result.has_more ? '; more are available upstream.' : '.'}
        </div>
        ${!imported && onExportSelected ? html`
          <button class="fluent-btn fluent-btn--primary" onClick=${onExportSelected} disabled=${exportDisabled}>Export selected traces</button>
        ` : ''}
      </div>

      <div class="fluent-card">
        <div class="fluent-card__body" style="padding: 0;">
          ${result.traces.length === 0 ? html`
            <div class="fluent-empty-state" style="padding: var(--space-xl);">
              <div class="fluent-empty-state__title">No matching traces</div>
              <p class="fluent-empty-state__text">Adjust the filters or time range and try again.</p>
            </div>
          ` : html`
            <div class="fluent-table-container" style="border: none;">
              <table class="fluent-table fluent-table--selectable">
                <thead>
                  <tr>
                    ${!imported ? html`<th>Select</th>` : ''}
                    <th>Service</th>
                    <th>Operation</th>
                    <th>Status</th>
                    <th style="text-align: right;">Spans</th>
                    <th style="text-align: right;">Duration</th>
                    <th>Start</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  ${renderRows(result.traces, imported, selectedTraceIds, selectedTraceId, onSelectTrace, onToggleTrace)}
                </tbody>
              </table>
            </div>
          `}
        </div>
      </div>
    </div>
  `;
}
