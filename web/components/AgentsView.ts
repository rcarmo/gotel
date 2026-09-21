import { h } from 'preact';
import { html } from 'htm/preact';
import type { Insights } from '../insights-types';
import { formatCount, formatDuration, formatPercent, formatUsd } from '../utils';

interface AgentsViewProps {
  insights: Insights | null;
  loading?: boolean;
  errorMessage?: string | null;
  onSelectTrace: (traceId: string) => void;
}

export function AgentsView({ insights, loading = false, errorMessage = null, onSelectTrace }: AgentsViewProps) {
  if (errorMessage) {
    return html`
      <div class="fluent-alert fluent-alert--error">
        <div class="fluent-alert__icon">⚠️</div>
        <div class="fluent-alert__content">
          <div class="fluent-alert__title">Unable to load agent view</div>
          <div class="fluent-alert__message">${errorMessage}</div>
        </div>
      </div>
    `;
  }

  if (loading && !insights) {
    return html`
      <div class="fluent-empty-state">
        <div class="fluent-empty-state__icon">⏳</div>
        <div class="fluent-empty-state__title">Loading agent spans</div>
      </div>
    `;
  }

  if (!insights || insights.agents.length === 0) {
    return html`
      <div class="fluent-empty-state">
        <div class="fluent-empty-state__icon">🤖</div>
        <div class="fluent-empty-state__title">No agent or LLM spans matched</div>
        <p class="fluent-empty-state__text">Agent aggregates always include matching agent spans even when the overview uses requests scope.</p>
      </div>
    `;
  }

  return html`
    <div class="gotel-stack">
      <div class="fluent-alert fluent-alert--info">
        <div class="fluent-alert__icon">🤖</div>
        <div class="fluent-alert__content">
          <div class="fluent-alert__title">Provider / model / operation view</div>
          <div class="fluent-alert__message">
            Prompt and response bodies are intentionally omitted here. Instrumented cost distinguishes unknown from true zero.
          </div>
        </div>
      </div>

      <div class="fluent-card">
        <div class="fluent-card__header">
          <h3 class="fluent-card__title">Agent activity</h3>
        </div>
        <div class="fluent-card__body" style="padding: 0;">
          <div class="fluent-table-container" style="border: none;">
            <table class="fluent-table">
              <thead>
                <tr>
                  <th>Provider / model</th>
                  <th>Service / operation</th>
                  <th style="text-align: right;">Count</th>
                  <th style="text-align: right;">Errors</th>
                  <th style="text-align: right;">Latency</th>
                  <th style="text-align: right;">Tokens</th>
                  <th style="text-align: right;">Cost</th>
                  <th>Retries / tools</th>
                  <th>Trace</th>
                </tr>
              </thead>
              <tbody>
                ${insights.agents.map((agent) => html`
                  <tr>
                    <td>
                      <div><code>${agent.provider || 'Unknown'}</code></div>
                      <div class="gotel-muted-text">${agent.model}</div>
                    </td>
                    <td>
                      <div><code>${agent.service}</code></div>
                      <div class="gotel-muted-text">${agent.operation}</div>
                    </td>
                    <td style="text-align: right;">${formatCount(agent.count)}</td>
                    <td style="text-align: right;">${formatCount(agent.error_count)}<div class="gotel-muted-text">${formatPercent(agent.count > 0 ? agent.error_count / agent.count : 0)}</div></td>
                    <td style="text-align: right;">p50 ${formatDuration(agent.p50_ms)}<div class="gotel-muted-text">p95 ${formatDuration(agent.p95_ms)}</div></td>
                    <td style="text-align: right;">
                      ${agent.token_samples > 0 ? html`${agent.input_token_samples ? formatCount(agent.input_tokens) : 'unknown'} in / ${agent.output_token_samples ? formatCount(agent.output_tokens) : 'unknown'} out<div class="gotel-muted-text">coverage: ${agent.input_token_samples}/${agent.count} in, ${agent.output_token_samples}/${agent.count} out</div>` : 'unknown'}
                    </td>
                    <td style="text-align: right;">${formatUsd(agent.cost_usd, agent.cost_samples > 0)}<div class="gotel-muted-text">${agent.cost_samples > 0 ? `${formatCount(agent.cost_samples)} sampled spans` : 'not instrumented'}</div></td>
                    <td>
                      <div>${formatCount(agent.retries)} retries</div>
                      <div class="gotel-muted-text">${agent.retry_samples > 0 ? `${formatCount(agent.retry_samples)} retry samples` : 'unknown'} · ${formatCount(agent.tool_failures)} tool failures / ${formatCount(agent.tool_calls)} tool calls</div>
                    </td>
                    <td>
                      <button class="fluent-btn fluent-btn--secondary fluent-btn--small" onClick=${() => onSelectTrace(agent.trace_id)}>Open trace</button>
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
