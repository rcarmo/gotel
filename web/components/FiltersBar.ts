import { h } from 'preact';
import { html } from 'htm/preact';
import type { InsightFilters } from '../insights-types';
import type { TimePreset } from '../query';
import { toDatetimeLocalValue } from '../query';

interface FiltersBarProps {
  filters: InsightFilters;
  timePreset: TimePreset;
  services: string[];
  disabled?: boolean;
  busy?: boolean;
  onChange: (next: InsightFilters) => void;
  onTimePresetChange: (preset: TimePreset) => void;
  onApply: () => void;
  onReset: () => void;
}

function updateNumber(input: string): number | undefined {
  if (input.trim() === '') {
    return undefined;
  }
  const parsed = Number(input);
  return Number.isFinite(parsed) ? parsed : undefined;
}

export function FiltersBar({ filters, timePreset, services, disabled = false, busy = false, onChange, onTimePresetChange, onApply, onReset }: FiltersBarProps) {
  const setField = <K extends keyof InsightFilters>(key: K, value: InsightFilters[K]) => {
    onChange({ ...filters, [key]: value });
  };

  return html`
    <section class="fluent-card gotel-filters-card" aria-label="Shared filters">
      <div class="fluent-card__header">
        <h2 class="fluent-card__title">Filters</h2>
      </div>
      <div class="fluent-card__body">
        <div class="gotel-filter-presets" role="group" aria-label="Time presets">
          ${(['1h', '6h', '24h', '7d', 'custom'] as TimePreset[]).map((preset) => html`
            <button
              class="fluent-btn ${timePreset === preset ? 'fluent-btn--primary' : 'fluent-btn--secondary'} fluent-btn--small"
              disabled=${disabled}
              onClick=${() => onTimePresetChange(preset)}
              type="button"
            >
              ${preset}
            </button>
          `)}
        </div>

        <div class="gotel-filter-grid">
          <label class="gotel-field">
            <span>From</span>
            <input
              class="gotel-input"
              type="datetime-local"
              value=${toDatetimeLocalValue(filters.from)}
              disabled=${disabled}
              onInput=${(event: Event) => {
                const next = (event.currentTarget as HTMLInputElement).value;
                const date = new Date(next);
                if (!Number.isNaN(date.getTime())) {
                  onTimePresetChange('custom');
                  setField('from', Math.floor(date.getTime() / 1000));
                }
              }}
            />
          </label>
          <label class="gotel-field">
            <span>To</span>
            <input
              class="gotel-input"
              type="datetime-local"
              value=${toDatetimeLocalValue(filters.to)}
              disabled=${disabled}
              onInput=${(event: Event) => {
                const next = (event.currentTarget as HTMLInputElement).value;
                const date = new Date(next);
                if (!Number.isNaN(date.getTime())) {
                  onTimePresetChange('custom');
                  setField('to', Math.floor(date.getTime() / 1000));
                }
              }}
            />
          </label>
          <label class="gotel-field">
            <span>Service</span>
            <input
              class="gotel-input"
              type="text"
              list="gotel-services"
              value=${filters.service ?? ''}
              disabled=${disabled}
              onInput=${(event: Event) => setField('service', (event.currentTarget as HTMLInputElement).value || undefined)}
              placeholder="Any service"
            />
            <datalist id="gotel-services">
              ${services.map((service) => html`<option value=${service}></option>`)}
            </datalist>
          </label>
          <label class="gotel-field">
            <span>Operation</span>
            <input
              class="gotel-input"
              type="text"
              value=${filters.operation ?? ''}
              disabled=${disabled}
              onInput=${(event: Event) => setField('operation', (event.currentTarget as HTMLInputElement).value || undefined)}
              placeholder="Exact span name"
            />
          </label>
          <label class="gotel-field">
            <span>Scope</span>
            <select
              class="gotel-input"
              value=${filters.scope ?? 'requests'}
              disabled=${disabled}
              onInput=${(event: Event) => setField('scope', (event.currentTarget as HTMLSelectElement).value as InsightFilters['scope'])}
            >
              <option value="requests">requests (root/server/consumer)</option>
              <option value="all">all spans</option>
            </select>
          </label>
          <label class="gotel-field gotel-field--checkbox">
            <span>Status</span>
            <label class="gotel-checkbox">
              <input
                type="checkbox"
                checked=${filters.status === 'error'}
                disabled=${disabled}
                onInput=${(event: Event) => setField('status', (event.currentTarget as HTMLInputElement).checked ? 'error' : undefined)}
              />
              <span>Error spans only</span>
            </label>
          </label>
          <label class="gotel-field">
            <span>Min duration (ms)</span>
            <input
              class="gotel-input"
              type="number"
              min="0"
              value=${filters.min_duration_ms ?? ''}
              disabled=${disabled}
              onInput=${(event: Event) => setField('min_duration_ms', updateNumber((event.currentTarget as HTMLInputElement).value))}
            />
          </label>
          <label class="gotel-field">
            <span>Max duration (ms)</span>
            <input
              class="gotel-input"
              type="number"
              min="0"
              value=${filters.max_duration_ms ?? ''}
              disabled=${disabled}
              onInput=${(event: Event) => setField('max_duration_ms', updateNumber((event.currentTarget as HTMLInputElement).value))}
            />
          </label>
          <label class="gotel-field">
            <span>Attribute</span>
            <input
              class="gotel-input"
              type="text"
              value=${filters.attribute ?? ''}
              disabled=${disabled}
              onInput=${(event: Event) => setField('attribute', (event.currentTarget as HTMLInputElement).value || undefined)}
              placeholder="http.method"
            />
          </label>
          <label class="gotel-field">
            <span>Value</span>
            <input
              class="gotel-input"
              type="text"
              value=${filters.value ?? ''}
              disabled=${disabled}
              onInput=${(event: Event) => setField('value', (event.currentTarget as HTMLInputElement).value)}
              placeholder="Exact attribute value (blank allowed)"
            />
          </label>
        </div>

        <div class="gotel-toolbar">
          <div class="gotel-muted-text">Time filters use Unix seconds and half-open windows [from, to).</div>
          <div class="gotel-toolbar__actions">
            <button class="fluent-btn fluent-btn--secondary" type="button" disabled=${disabled} onClick=${onReset}>Reset</button>
            <button class="fluent-btn fluent-btn--primary" type="button" disabled=${disabled || busy} onClick=${onApply}>${busy ? 'Applying…' : 'Apply filters'}</button>
          </div>
        </div>
      </div>
    </section>
  `;
}
