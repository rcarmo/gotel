import { h } from 'preact';
import { html } from 'htm/preact';
import type { InsightBucket, Insights } from '../insights-types';
import { formatCount, formatDateTimeFromSeconds, formatDuration, formatPercent, formatRate } from '../utils';

interface OverviewViewProps {
  insights: Insights | null;
  loading?: boolean;
  errorMessage?: string | null;
  imported?: boolean;
  onSelectTrace: (traceId: string) => void;
  onOpenBucket: (bucket: InsightBucket) => void;
}

function recurrenceLabel(current: number, previous: number | null): { text: string; tone: string } {
  if (previous === null) {
    return { text: 'No comparison window', tone: 'info' };
  }
  if (previous === 0 && current > 0) {
    return { text: 'New vs previous window', tone: 'error' };
  }
  if (current > previous) {
    return { text: `Up ${current - previous}`, tone: 'error' };
  }
  if (current < previous) {
    return { text: `Down ${previous - current}`, tone: 'success' };
  }
  return { text: 'Flat versus previous window', tone: 'info' };
}

export function OverviewView({ insights, loading = false, errorMessage = null, imported = false, onSelectTrace, onOpenBucket }: OverviewViewProps) {
  if (errorMessage) {
    return html`
      <div class="fluent-alert fluent-alert--error">
        <div class="fluent-alert__icon">⚠️</div>
        <div class="fluent-alert__content">
          <div class="fluent-alert__title">Unable to load overview</div>
          <div class="fluent-alert__message">${errorMessage}</div>
        </div>
      </div>
    `;
  }

  if (loading && !insights) {
    return html`
      <div class="fluent-empty-state">
        <div class="fluent-empty-state__icon">⏳</div>
        <div class="fluent-empty-state__title">Loading overview</div>
        <p class="fluent-empty-state__text">Aggregating spans for the selected time window.</p>
      </div>
    `;
  }

  if (!insights) {
    return html`
      <div class="fluent-empty-state">
        <div class="fluent-empty-state__icon">📊</div>
        <div class="fluent-empty-state__title">No overview loaded</div>
        <p class="fluent-empty-state__text">Apply a time range to load native insights.</p>
      </div>
    `;
  }

  const topErrors = insights.errors.slice(0, 8);
  const topOperations = insights.operations.slice(0, 10);
  const services = insights.services;

  return html`
    <div class="gotel-stack">
      <div class="fluent-alert fluent-alert--info">
        <div class="fluent-alert__icon">ℹ️</div>
        <div class="fluent-alert__content">
          <div class="fluent-alert__title">Native overview${imported ? ' (imported bundle)' : ''}</div>
          <div class="fluent-alert__message">
            Metrics are computed from sampled spans scanned by the backend (${formatCount(insights.scanned_spans)} of up to ${formatCount(insights.max_spans)} per window), not true request throughput.
            Requests scope focuses on root/server/consumer style spans; all spans includes every matching span. Error groups include all matching error spans, including internal spans, even when requests scope is selected.
          </div>
        </div>
      </div>

      <div class="fluent-grid fluent-grid--4">
        <div class="gotel-stat-card">
          <div class="gotel-stat-card__value">${formatCount(insights.summary.count)}</div>
          <div class="gotel-stat-card__label">Sampled spans</div>
          <div class="gotel-stat-card__subtext">${formatRate(insights.summary.rate_per_second)}</div>
        </div>
        <div class="gotel-stat-card ${insights.summary.error_count > 0 ? 'gotel-stat-card--error' : 'gotel-stat-card--success'}">
          <div class="gotel-stat-card__value">${formatCount(insights.summary.error_count)}</div>
          <div class="gotel-stat-card__label">Errors</div>
          <div class="gotel-stat-card__subtext">${formatPercent(insights.summary.error_rate)}</div>
        </div>
        <div class="gotel-stat-card">
          <div class="gotel-stat-card__value">${formatDuration(insights.summary.p95_ms)}</div>
          <div class="gotel-stat-card__label">p95 latency</div>
          <div class="gotel-stat-card__subtext">p50 ${formatDuration(insights.summary.p50_ms)}</div>
        </div>
        <div class="gotel-stat-card">
          <div class="gotel-stat-card__value">${formatDuration(insights.summary.p99_ms)}</div>
          <div class="gotel-stat-card__label">p99 latency</div>
          <div class="gotel-stat-card__subtext">sum ${formatDuration(insights.summary.duration_sum_ms)}</div>
        </div>
      </div>

      <div class="fluent-card">
        <div class="fluent-card__header">
          <h3 class="fluent-card__title">Error groups vs previous window</h3>
        </div>
        <div class="fluent-card__body" style="padding: 0;">
          ${topErrors.length === 0 ? html`
            <div class="fluent-empty-state" style="padding: var(--space-xl);">
              <div class="fluent-empty-state__title">No grouped errors in this window</div>
            </div>
          ` : html`
            <div class="fluent-table-container" style="border: none;">
              <table class="fluent-table">
                <thead>
                  <tr>
                    <th>Service / operation</th>
                    <th>Error type</th>
                    <th style="text-align: right;">Current</th>
                    <th style="text-align: right;">Previous</th>
                    <th>Recurrence</th>
                    <th>Representative trace</th>
                  </tr>
                </thead>
                <tbody>
                  ${topErrors.map((error) => {
                    const recurrence = recurrenceLabel(error.count, error.previous_count);
                    return html`
                      <tr>
                        <td>
                          <div><code>${error.service}</code></div>
                          <div class="gotel-muted-text">${error.operation}</div>
                        </td>
                        <td>
                          <div>${error.type}</div>
                          <div class="gotel-muted-text">first seen ${formatDateTimeFromSeconds(error.first_seen)}</div>
                          <div class="gotel-muted-text">${error.versions.join(', ') || 'no versions'}</div>
                        </td>
                        <td style="text-align: right;">${formatCount(error.count)}</td>
                        <td style="text-align: right;">${error.previous_count === null ? '—' : formatCount(error.previous_count)}</td>
                        <td>
                          <span class="fluent-badge ${recurrence.tone === 'error' ? 'fluent-badge--tint-error' : recurrence.tone === 'success' ? 'fluent-badge--tint-success' : 'fluent-badge--tint-info'}">${recurrence.text}</span>
                          <div class="gotel-muted-text">last seen ${formatDateTimeFromSeconds(error.last_seen)}</div>
                        </td>
                        <td>
                          <button class="fluent-btn fluent-btn--secondary fluent-btn--small" onClick=${() => onSelectTrace(error.trace_id)}>Open trace</button>
                        </td>
                      </tr>
                    `;
                  })}
                </tbody>
              </table>
            </div>
          `}
        </div>
      </div>

      <div class="fluent-card">
        <div class="fluent-card__header">
          <h3 class="fluent-card__title">Slow operations</h3>
        </div>
        <div class="fluent-card__body" style="padding: 0;">
          <div class="fluent-table-container" style="border: none;">
            <table class="fluent-table">
              <thead>
                <tr>
                  <th>Service</th>
                  <th>Operation</th>
                  <th style="text-align: right;">Count</th>
                  <th style="text-align: right;">p95</th>
                  <th style="text-align: right;">Previous p95</th>
                  <th style="text-align: right;">Δ</th>
                  <th>Representative trace</th>
                </tr>
              </thead>
              <tbody>
                ${topOperations.map((operation) => html`
                  <tr>
                    <td><code>${operation.service}</code></td>
                    <td>${operation.operation}</td>
                    <td style="text-align: right;">${formatCount(operation.count)}</td>
                    <td style="text-align: right;">${formatDuration(operation.p95_ms)}</td>
                    <td style="text-align: right;">${operation.previous_p95_ms === null ? '—' : formatDuration(operation.previous_p95_ms)}</td>
                    <td style="text-align: right;">
                      ${operation.change_percent === null ? '—' : html`<span class="${operation.change_percent > 0 ? 'gotel-text-error' : operation.change_percent < 0 ? 'gotel-text-success' : ''}">${operation.change_percent.toFixed(1)}%</span>`}
                    </td>
                    <td>
                      <button class="fluent-btn fluent-btn--secondary fluent-btn--small" onClick=${() => onSelectTrace(operation.trace_id)}>Open trace</button>
                    </td>
                  </tr>
                `)}
              </tbody>
            </table>
          </div>
        </div>
      </div>

      <div class="fluent-grid fluent-grid--2">
        <div class="fluent-card" style="margin: 0;">
          <div class="fluent-card__header">
            <h3 class="fluent-card__title">Time buckets</h3>
          </div>
          <div class="fluent-card__body">
            <p class="gotel-muted-text">Observed spans over time. Select a bar to search its traces.</p>
            <div class="gotel-overview-chart" role="group" aria-label="Span counts by time bucket">
              ${insights.buckets.map((bucket) => html`
                <button class="gotel-overview-bar"
                  style=${`height: ${Math.max(3, 100 * bucket.count / Math.max(1, ...insights.buckets.map(b => b.count)))}%;`}
                  title=${`${formatDateTimeFromSeconds(bucket.from)}: ${bucket.count} spans, ${bucket.error_count} errors`}
                  aria-label=${`${formatDateTimeFromSeconds(bucket.from)}: ${bucket.count} spans`}
                  onClick=${() => onOpenBucket(bucket)}></button>
              `)}
            </div>
            <div class="gotel-toolbar gotel-muted-text"><span>${formatDateTimeFromSeconds(insights.from)}</span><span>${formatDateTimeFromSeconds(insights.to)}</span></div>
          </div>
        </div>

        <div class="fluent-card" style="margin: 0;">
          <div class="fluent-card__header">
            <h3 class="fluent-card__title">Services last seen</h3>
          </div>
          <div class="fluent-card__body">
            <p class="gotel-muted-text">Activity timing only, not health. This list uses the service and time filters; other filters apply only to the displayed versions.</p>
            <div class="gotel-service-list" style="max-height: 24rem; overflow-y: auto;">
              ${services.map((service) => html`
                <div class="gotel-service-row">
                  <div style="min-width: 0;">
                    <div style="overflow-wrap: anywhere;"><code>${service.service}</code></div>
                    <div class="gotel-muted-text">last seen ${formatDateTimeFromSeconds(service.last_seen)}</div>
                  </div>
                  <div style="text-align: right; min-width: 0;">
                    <span class="fluent-badge ${service.in_window ? 'fluent-badge--tint-info' : 'fluent-badge--tint-warning'}">${service.in_window ? 'Seen in window' : 'Seen outside window'}</span>
                    <div class="gotel-muted-text" style="overflow-wrap: anywhere;">Matching versions: ${service.versions.join(', ') || 'none recorded'}</div>
                  </div>
                </div>
              `)}
            </div>
          </div>
        </div>
      </div>
    </div>
  `;
}
