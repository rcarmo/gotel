import { h } from 'preact';
import { html } from 'htm/preact';
import type { Exception } from '../state';

interface ExceptionsViewProps {
  exceptions: Exception[];
}

// Render exceptions view - Azure Alert Style
export function ExceptionsView({ exceptions }: ExceptionsViewProps) {
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
  
  // Pre-calculate exception counts for performance optimization
  const criticalCount = exceptions.filter(e => e.severity === 'critical').length;
  const warningCount = exceptions.filter(e => e.severity === 'warning').length;
  const infoCount = exceptions.filter(e => e.severity === 'info').length;
  
  // Group exceptions by service and type
  const exceptionsByService = new Map<string, { service: string; type: string; exceptions: Exception[] }>();
  exceptions.forEach(exc => {
    const key = `${exc.service_name}|${exc.exception_type || 'Unknown'}`;
    if (!exceptionsByService.has(key)) {
      exceptionsByService.set(key, {
        service: exc.service_name,
        type: exc.exception_type || 'Unknown',
        exceptions: []
      });
    }
    exceptionsByService.get(key)!.exceptions.push(exc);
  });
  
  return html`
    <div>
      <!-- Exception Summary -->
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
      
      <!-- Exception Groups -->
      ${Array.from(exceptionsByService.values()).map(group => html`
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
            ${group.exceptions.map((exc, index) => html`
              <div style="padding: var(--space-l); ${index < group.exceptions.length - 1 ? 'border-bottom: 1px solid var(--color-border);' : ''}">
                <div style="display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: var(--space-m);">
                  <div>
                    <div class="fluent-subtitle2" style="color: var(--fluent-error);">
                      ${exc.exception_type || 'Exception'} in ${exc.span_name}
                    </div>
                    <div class="fluent-caption1" style="display: flex; gap: var(--space-l); margin-top: var(--space-xs);">
                      <span>🏷️ ${exc.service_name}</span>
                      <span>🕐 ${new Date(exc.timestamp).toLocaleString()}</span>
                      <span>🆔 ${exc.trace_id.slice(0, 8)}...</span>
                    </div>
                  </div>
                  <span class="fluent-badge ${exc.severity === 'critical' ? 'fluent-badge--filled-error' : exc.severity === 'warning' ? 'fluent-badge--filled-warning' : 'fluent-badge--tint-error'}">
                    ${(exc.severity || 'ERROR').toUpperCase()}
                  </span>
                </div>
                
                ${exc.message ? html`
                  <div class="fluent-alert fluent-alert--error" style="margin-bottom: var(--space-m);">
                    <div class="fluent-alert__content">
                      <div class="fluent-alert__message">${exc.message}</div>
                    </div>
                  </div>
                ` : ''}
                
                ${exc.stack_trace ? html`
                  <details style="margin-bottom: var(--space-m);">
                    <summary>Stack Trace</summary>
                    <pre class="fluent-pre" style="margin-top: var(--space-s);">${exc.stack_trace}</pre>
                  </details>
                ` : ''}
                
                <div style="display: flex; gap: var(--space-s);">
                  <button 
                    class="fluent-btn fluent-btn--secondary fluent-btn--small"
                    onclick="navigator.clipboard.writeText('${exc.trace_id}'); alert('Trace ID copied!');"
                  >
                    📋 Copy Trace ID
                  </button>
                  <button 
                    class="fluent-btn fluent-btn--primary fluent-btn--small"
                    onclick="window.location.href='/?trace=${exc.trace_id}';"
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