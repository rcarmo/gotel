import { h } from 'preact';
import { html } from 'htm/preact';
import type { Exception } from '../state';

interface ExceptionsViewProps {
  exceptions: Exception[];
  onSelectTrace: (traceId: string) => void;
  errorMessage?: string | null;
}

// Render exceptions view - Azure Alert Style
export function ExceptionsView({ exceptions, onSelectTrace, errorMessage = null }: ExceptionsViewProps) {
  if (errorMessage) {
    return html`
      <div class="fluent-alert fluent-alert--error">
        <div class="fluent-alert__icon">⚠️</div>
        <div class="fluent-alert__content">
          <div class="fluent-alert__title">Unable to Load Exceptions</div>
          <div class="fluent-alert__message">${errorMessage}</div>
        </div>
      </div>
    `;
  }

  if (exceptions.length === 0) {
    return html`
      <div class="fluent-empty-state">
        <div class="fluent-empty-state__icon">✅</div>
        <div class="fluent-empty-state__title">No Exceptions Found</div>
        <p class="fluent-empty-state__text">
          No exceptions have been recorded in the current time range.
        </p>
      </div>
    `;
  }

  const criticalCount = exceptions.filter((exception) => exception.severity === 'critical').length;
  const warningCount = exceptions.filter((exception) => exception.severity === 'warning').length;
  const infoCount = exceptions.filter((exception) => exception.severity === 'info').length;

  const exceptionsByService = new Map<string, { service: string; type: string; exceptions: Exception[] }>();
  exceptions.forEach((exception) => {
    const key = `${exception.service_name}|${exception.exception_type || 'Unknown'}`;
    if (!exceptionsByService.has(key)) {
      exceptionsByService.set(key, {
        service: exception.service_name,
        type: exception.exception_type || 'Unknown',
        exceptions: []
      });
    }
    exceptionsByService.get(key)?.exceptions.push(exception);
  });

  return html`
    <div>
      <div class="fluent-grid fluent-grid--3" style="margin-bottom: var(--space-xl);">
        <div class="gotel-stat-card gotel-stat-card--error">
          <div class="gotel-stat-card__value">${criticalCount}</div>
          <div class="gotel-stat-card__label">Critical</div>
        </div>
        <div class="gotel-stat-card gotel-stat-card--warning">
          <div class="gotel-stat-card__value">${warningCount}</div>
          <div class="gotel-stat-card__label">Warning</div>
        </div>
        <div class="gotel-stat-card">
          <div class="gotel-stat-card__value">${infoCount}</div>
          <div class="gotel-stat-card__label">Info</div>
        </div>
      </div>

      ${Array.from(exceptionsByService.values()).map((group) => html`
        <div class="fluent-card" style="margin-bottom: var(--space-l);">
          <div class="fluent-card__header" style="background: var(--fluent-error-bg);">
            <div style="display: flex; align-items: center; gap: var(--space-s);">
              <span style="font-size: 20px;">⚠️</span>
              <div>
                <div class="fluent-subtitle2">${group.service}</div>
                <div class="fluent-caption1">${group.type} · ${group.exceptions.length} occurrence${group.exceptions.length > 1 ? 's' : ''}</div>
              </div>
            </div>
          </div>

          <div class="fluent-card__body" style="padding: 0;">
            ${group.exceptions.map((exception, index) => html`
              <div style="padding: var(--space-l); ${index < group.exceptions.length - 1 ? 'border-bottom: 1px solid var(--color-border);' : ''}">
                <div style="display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: var(--space-m);">
                  <div>
                    <div class="fluent-subtitle2" style="color: var(--fluent-error);">
                      ${exception.exception_type || 'Exception'} in ${exception.span_name}
                    </div>
                    <div class="fluent-caption1" style="display: flex; gap: var(--space-l); margin-top: var(--space-xs);">
                      <span>🏷️ ${exception.service_name}</span>
                      <span>🕐 ${new Date(exception.timestamp).toLocaleString()}</span>
                      <span>🆔 ${exception.trace_id.slice(0, 8)}...</span>
                    </div>
                  </div>
                  <span class="fluent-badge ${exception.severity === 'critical' ? 'fluent-badge--filled-error' : exception.severity === 'warning' ? 'fluent-badge--filled-warning' : 'fluent-badge--tint-error'}">
                    ${(exception.severity || 'ERROR').toUpperCase()}
                  </span>
                </div>

                ${exception.message ? html`
                  <div class="fluent-alert fluent-alert--error" style="margin-bottom: var(--space-m);">
                    <div class="fluent-alert__content">
                      <div class="fluent-alert__message">${exception.message}</div>
                    </div>
                  </div>
                ` : ''}

                ${exception.stack_trace ? html`
                  <details style="margin-bottom: var(--space-m);">
                    <summary>Stack Trace</summary>
                    <pre class="fluent-pre" style="margin-top: var(--space-s);">${exception.stack_trace}</pre>
                  </details>
                ` : ''}

                <div style="display: flex; gap: var(--space-s);">
                  <button
                    class="fluent-btn fluent-btn--secondary fluent-btn--small"
                    onClick=${(event: Event) => {
                      const button = event.currentTarget as HTMLButtonElement;
                      navigator.clipboard.writeText(exception.trace_id).then(() => {
                        const originalText = button.innerText;
                        button.innerText = '✅ Copied!';
                        setTimeout(() => {
                          button.innerText = originalText;
                        }, 2000);
                      });
                    }}
                  >
                    📋 Copy Trace ID
                  </button>
                  <button
                    class="fluent-btn fluent-btn--primary fluent-btn--small"
                    onClick=${() => onSelectTrace(exception.trace_id)}
                  >
                    🔍 View Trace
                  </button>
                </div>
              </div>
            `)}
          </div>
        </div>
      `)}
    </div>
  `;
}
