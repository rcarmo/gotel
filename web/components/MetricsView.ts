import { h } from 'preact';
import { html } from 'htm/preact';
import type { InsightBucket, Insights } from '../insights-types';
import { formatCount, formatDateTimeFromSeconds, formatDuration, formatPercent, formatRate } from '../utils';

interface MetricsViewProps {
  insights: Insights | null;
  loading?: boolean;
  errorMessage?: string | null;
  onOpenBucket: (bucket: InsightBucket) => void;
}

const HISTOGRAM_LABELS = ['<10ms', '[10,50)ms', '[50,100)ms', '[100,500)ms', '[500,1000)ms', '≥1000ms'];

export function MetricsView({ insights, loading = false, errorMessage = null, onOpenBucket }: MetricsViewProps) {
  if (errorMessage) {
    return html`
      <div class="fluent-alert fluent-alert--error">
        <div class="fluent-alert__icon">⚠️</div>
        <div class="fluent-alert__content">
          <div class="fluent-alert__title">Unable to load metrics</div>
          <div class="fluent-alert__message">${errorMessage}</div>
        </div>
      </div>
    `;
  }

  if (loading && !insights) {
    return html`
      <div class="fluent-empty-state">
        <div class="fluent-empty-state__icon">⏳</div>
        <div class="fluent-empty-state__title">Loading metrics</div>
      </div>
    `;
  }

  if (!insights) {
    return html`
      <div class="fluent-empty-state">
        <div class="fluent-empty-state__icon">📈</div>
        <div class="fluent-empty-state__title">No metrics available</div>
      </div>
    `;
  }

  const histogramMax = Math.max(1, ...insights.summary.histogram);
  const bucketMax = Math.max(1, ...insights.buckets.map((bucket) => bucket.count));

  return html`
    <div class="gotel-stack">
      <div class="fluent-alert fluent-alert--info">
        <div class="fluent-alert__icon">📌</div>
        <div class="fluent-alert__content">
          <div class="fluent-alert__title">Sampled span metrics</div>
          <div class="fluent-alert__message">
            Counts, rates and latency percentiles come from sampled spans in the selected scope, not end-to-end request throughput. Click a time bucket to open matching trace search.
          </div>
        </div>
      </div>

      <div class="fluent-grid fluent-grid--4">
        <div class="gotel-stat-card">
          <div class="gotel-stat-card__value">${formatCount(insights.summary.count)}</div>
          <div class="gotel-stat-card__label">Count</div>
          <div class="gotel-stat-card__subtext">${formatRate(insights.summary.rate_per_second)}</div>
        </div>
        <div class="gotel-stat-card ${insights.summary.error_count > 0 ? 'gotel-stat-card--error' : ''}">
          <div class="gotel-stat-card__value">${formatPercent(insights.summary.error_rate)}</div>
          <div class="gotel-stat-card__label">Error rate</div>
          <div class="gotel-stat-card__subtext">${formatCount(insights.summary.error_count)} error spans</div>
        </div>
        <div class="gotel-stat-card">
          <div class="gotel-stat-card__value">${formatDuration(insights.summary.p50_ms)}</div>
          <div class="gotel-stat-card__label">p50</div>
          <div class="gotel-stat-card__subtext">p95 ${formatDuration(insights.summary.p95_ms)}</div>
        </div>
        <div class="gotel-stat-card">
          <div class="gotel-stat-card__value">${formatDuration(insights.summary.p99_ms)}</div>
          <div class="gotel-stat-card__label">p99</div>
          <div class="gotel-stat-card__subtext">sum ${formatDuration(insights.summary.duration_sum_ms)}</div>
        </div>
      </div>

      <div class="fluent-grid fluent-grid--2">
        <div class="fluent-card" style="margin: 0;">
          <div class="fluent-card__header">
            <h3 class="fluent-card__title">Time buckets</h3>
          </div>
          <div class="fluent-card__body">
            <div class="gotel-bucket-chart">
              ${insights.buckets.map((bucket) => html`
                <button class="gotel-chart-row" onClick=${() => onOpenBucket(bucket)}>
                  <div class="gotel-chart-row__label">${formatDateTimeFromSeconds(bucket.from)}</div>
                  <div class="gotel-chart-row__bar-wrap">
                    <div class="gotel-chart-row__bar" style=${`width: ${(bucket.count / bucketMax) * 100}%;`}></div>
                  </div>
                  <div class="gotel-chart-row__value">${formatCount(bucket.count)} · ${formatPercent(bucket.error_rate)} · p95 ${formatDuration(bucket.p95_ms)}</div>
                </button>
              `)}
            </div>
          </div>
        </div>

        <div class="fluent-card" style="margin: 0;">
          <div class="fluent-card__header">
            <h3 class="fluent-card__title">Latency histogram</h3>
          </div>
          <div class="fluent-card__body">
            <div class="gotel-bucket-chart">
              ${insights.summary.histogram.map((count, index) => html`
                <div class="gotel-chart-row gotel-chart-row--static">
                  <div class="gotel-chart-row__label">${HISTOGRAM_LABELS[index] ?? `Bucket ${index + 1}`}</div>
                  <div class="gotel-chart-row__bar-wrap">
                    <div class="gotel-chart-row__bar gotel-chart-row__bar--warning" style=${`width: ${(count / histogramMax) * 100}%;`}></div>
                  </div>
                  <div class="gotel-chart-row__value">${formatCount(count)}</div>
                </div>
              `)}
            </div>
          </div>
        </div>
      </div>

      <div class="fluent-card">
        <div class="fluent-card__header">
          <h3 class="fluent-card__title">Bucket detail</h3>
        </div>
        <div class="fluent-card__body" style="padding: 0;">
          <div class="fluent-table-container" style="border: none;">
            <table class="fluent-table">
              <thead>
                <tr>
                  <th>Window</th>
                  <th style="text-align: right;">Count</th>
                  <th style="text-align: right;">Error rate</th>
                  <th style="text-align: right;">p50</th>
                  <th style="text-align: right;">p95</th>
                  <th style="text-align: right;">p99</th>
                  <th>Representative trace</th>
                </tr>
              </thead>
              <tbody>
                ${insights.buckets.map((bucket) => html`
                  <tr>
                    <td>${formatDateTimeFromSeconds(bucket.from)} → ${formatDateTimeFromSeconds(bucket.to)}</td>
                    <td style="text-align: right;">${formatCount(bucket.count)}</td>
                    <td style="text-align: right;">${formatPercent(bucket.error_rate)}</td>
                    <td style="text-align: right;">${formatDuration(bucket.p50_ms)}</td>
                    <td style="text-align: right;">${formatDuration(bucket.p95_ms)}</td>
                    <td style="text-align: right;">${formatDuration(bucket.p99_ms)}</td>
                    <td>
                      <button class="fluent-btn fluent-btn--secondary fluent-btn--small" onClick=${() => onOpenBucket(bucket)}>Open matching traces</button>
                    </td>
                  </tr>
                `)}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  `;
}
