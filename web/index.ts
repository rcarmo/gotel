import { h, render } from 'preact';
import { useCallback, useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { html } from 'htm/preact';
import { AgentsView } from './components/AgentsView';
import { ExploreView } from './components/ExploreView';
import { FiltersBar } from './components/FiltersBar';
import { MetricsView } from './components/MetricsView';
import { OverviewView } from './components/OverviewView';
import { TimelineView } from './components/TimelineView';
import {
  buildImportedBundleData,
  buildImportedExploreResult,
  INVESTIGATION_BUNDLE_BYTE_LIMIT,
  validateInvestigationBundle,
  type ImportedBundleData
} from './bundles';
import type {
  ExploreResult,
  InsightBucket,
  InsightFilters,
  Insights,
  InvestigationBundle
} from './insights-types';
import {
  buildExploreUrl,
  buildHash,
  buildInsightsUrl,
  buildInvestigationUrl,
  detectPreset,
  getPresetRange,
  normalizeFilters,
  parseHash,
  type TimePreset
} from './query';
import { normalizeSpan, type Span, type StoredSpan, type ViewMode } from './state';

const navItems: Array<{ id: ViewMode; icon: string; label: string }> = [
  { id: 'overview', icon: '🏠', label: 'Overview' },
  { id: 'explore', icon: '🔎', label: 'Trace Search' },
  { id: 'timeline', icon: '⏱️', label: 'Timeline' },
  { id: 'metrics', icon: '📈', label: 'Metrics' },
  { id: 'agents', icon: '🤖', label: 'Agents' }
];

const DEFAULT_PRESET: Exclude<TimePreset, 'custom'> = '24h';
const DEFAULT_FILTERS = getPresetRange(DEFAULT_PRESET);

function isViewMode(value: string | undefined): value is ViewMode {
  return value === 'overview' || value === 'explore' || value === 'timeline' || value === 'metrics' || value === 'agents';
}

function getViewTitle(viewMode: ViewMode): string {
  switch (viewMode) {
    case 'overview':
      return 'Overview';
    case 'explore':
      return 'Trace Search';
    case 'timeline':
      return 'Trace Timeline';
    case 'metrics':
      return 'Metrics';
    case 'agents':
      return 'Agents';
  }
}

function uniq(values: string[]): string[] {
  return [...new Set(values.filter((value) => value.trim().length > 0))].sort((a, b) => a.localeCompare(b));
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function parseServicesPayload(payload: unknown): string[] {
  if (Array.isArray(payload) && payload.every((entry) => typeof entry === 'string')) {
    return uniq(payload);
  }
  if (isRecord(payload) && Array.isArray(payload.tagValues) && payload.tagValues.every((entry) => typeof entry === 'string')) {
    return uniq(payload.tagValues as string[]);
  }
  throw new Error('Invalid services response from backend.');
}

async function readErrorMessage(response: Response): Promise<string> {
  const contentType = response.headers.get('content-type') || '';

  if (contentType.includes('application/json')) {
    try {
      const payload = await response.json() as { error?: string; message?: string };
      return payload.error || payload.message || `Request failed with status ${response.status}`;
    } catch {
      return `Request failed with status ${response.status}`;
    }
  }

  const text = await response.text();
  return text || `Request failed with status ${response.status}`;
}

async function fetchJson<T>(url: string): Promise<T> {
  const response = await fetch(url, { headers: { Accept: 'application/json' } });
  if (!response.ok) {
    throw new Error(await readErrorMessage(response));
  }
  return response.json() as Promise<T>;
}

function downloadJson(filename: string, value: unknown) {
  const blob = new Blob([JSON.stringify(value, null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

function bundleFilename(bundle: InvestigationBundle): string {
  const stamp = bundle.created_at.replaceAll(':', '').replaceAll('.', '-');
  return `gotel-investigation-${stamp}.json`;
}

function App() {
  const initialHash = typeof window !== 'undefined' ? parseHash(window.location.hash) : {};
  const initialView = isViewMode(initialHash.view) ? initialHash.view : 'overview';

  const [viewMode, setViewMode] = useState<ViewMode>(initialView);
  const [menuOpen, setMenuOpen] = useState(false);
  const menuToggleRef = useRef<HTMLButtonElement>(null);
  const closeMenu = useCallback(() => {
    setMenuOpen(false);
    menuToggleRef.current?.focus();
  }, []);
  useEffect(() => {
    const desktop = window.matchMedia('(min-width: 769px)');
    const onResize = () => { if (desktop.matches) setMenuOpen(false); };
    desktop.addEventListener('change', onResize);
    return () => desktop.removeEventListener('change', onResize);
  }, []);
  useEffect(() => {
    if (!menuOpen) return;
    const navigation = document.getElementById('main-navigation');
    navigation?.querySelector<HTMLButtonElement>('button')?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        closeMenu();
      } else if (event.key === 'Tab') {
        const controls = [menuToggleRef.current, ...Array.from(navigation?.querySelectorAll<HTMLElement>('button, a[href]') ?? [])]
          .filter((element): element is HTMLElement => element !== null);
        const index = controls.indexOf(document.activeElement as HTMLElement);
        event.preventDefault();
        controls[(index + (event.shiftKey ? controls.length - 1 : 1)) % controls.length]?.focus();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [menuOpen, closeMenu]);
  const [appliedFilters, setAppliedFilters] = useState<InsightFilters>(DEFAULT_FILTERS);
  const [draftFilters, setDraftFilters] = useState<InsightFilters>(DEFAULT_FILTERS);
  const [timePreset, setTimePreset] = useState<TimePreset>(DEFAULT_PRESET);
  const [appliedPreset, setAppliedPreset] = useState<TimePreset>(DEFAULT_PRESET);
  const [services, setServices] = useState<string[]>([]);
  const [insights, setInsights] = useState<Insights | null>(null);
  const [insightsLoading, setInsightsLoading] = useState(false);
  const [insightsError, setInsightsError] = useState<string | null>(null);
  const [explore, setExplore] = useState<ExploreResult | null>(null);
  const [exploreLoading, setExploreLoading] = useState(false);
  const [exploreError, setExploreError] = useState<string | null>(null);
  const [selectedTraceId, setSelectedTraceId] = useState<string | null>(initialHash.traceId ?? null);
  const [selectedTraceSpans, setSelectedTraceSpans] = useState<Span[]>([]);
  const [selectedTraceLoading, setSelectedTraceLoading] = useState(false);
  const [selectedTraceError, setSelectedTraceError] = useState<string | null>(null);
  const [selectedTraceIds, setSelectedTraceIds] = useState<Set<string>>(new Set());
  const [traceInput, setTraceInput] = useState('');
  const [globalError, setGlobalError] = useState<string | null>(null);
  const [globalInfo, setGlobalInfo] = useState<string | null>(null);
  const [servicesError, setServicesError] = useState<string | null>(null);
  const [exporting, setExporting] = useState(false);
  const [importedData, setImportedData] = useState<ImportedBundleData | null>(null);
  const [traceReloadKey, setTraceReloadKey] = useState(0);

  const fileInputRef = useRef<HTMLInputElement>(null);
  const traceRequestIdRef = useRef(0);
  const requests = useRef({ services: 0, insights: 0, explore: 0 });
  const offlineRef = useRef(false);
  const liveSnapshotRef = useRef<{
    filters: InsightFilters;
    draftFilters: InsightFilters;
    timePreset: TimePreset;
    appliedPreset: TimePreset;
    viewMode: ViewMode;
    selectedTraceId: string | null;
  } | null>(null);

  const imported = importedData !== null;
  const exploreResult = useMemo(
    () => (importedData ? buildImportedExploreResult(importedData, appliedFilters) : explore),
    [appliedFilters, explore, importedData]
  );

  const updateHash = useCallback((nextView: ViewMode, nextTraceId: string | null) => {
    if (typeof window === 'undefined') {
      return;
    }
    const nextHash = buildHash({ view: nextView, traceId: nextTraceId ?? undefined });
    if (window.location.hash !== nextHash) {
      window.history.replaceState(null, '', nextHash);
    }
  }, []);

  const selectTrace = useCallback((traceId: string, nextView: ViewMode = 'timeline') => {
    setSelectedTraceId(traceId);
    setViewMode(nextView);
    setSelectedTraceIds((prev) => {
      if (!prev.has(traceId)) {
        return prev;
      }
      return new Set(prev);
    });
  }, []);

  const loadServices = useCallback(async () => {
    const id = ++requests.current.services;
    if (offlineRef.current) {
      return;
    }
    try {
      const payload = await fetchJson<unknown>('/api/services');
      if (id !== requests.current.services || offlineRef.current) return;
      setServices(parseServicesPayload(payload));
      setServicesError(null);
    } catch (error) {
      if (id !== requests.current.services || offlineRef.current) return;
      setServices([]);
      setServicesError((error as Error).message);
    }
  }, [importedData]);

  const loadInsights = useCallback(async (filters: InsightFilters) => {
    const id = ++requests.current.insights;
    if (offlineRef.current) {
      return;
    }
    setInsightsLoading(true);
    setInsightsError(null);
    try {
      const payload = await fetchJson<Insights>(buildInsightsUrl(filters));
      if (id !== requests.current.insights || offlineRef.current) return;
      setInsights(payload);
    } catch (error) {
      if (id !== requests.current.insights || offlineRef.current) return;
      setInsights(null);
      setInsightsError((error as Error).message);
    } finally {
      if (id === requests.current.insights && !offlineRef.current) setInsightsLoading(false);
    }
  }, [importedData]);

  const loadExplore = useCallback(async (filters: InsightFilters) => {
    const id = ++requests.current.explore;
    if (offlineRef.current) {
      return;
    }
    setExploreLoading(true);
    setExploreError(null);
    try {
      const payload = await fetchJson<ExploreResult>(buildExploreUrl(filters, { limit: 100 }));
      if (id !== requests.current.explore || offlineRef.current) return;
      setExplore(payload);
    } catch (error) {
      if (id !== requests.current.explore || offlineRef.current) return;
      setExplore(null);
      setExploreError((error as Error).message);
    } finally {
      if (id === requests.current.explore && !offlineRef.current) setExploreLoading(false);
    }
  }, [importedData]);

  const loadSelectedTrace = useCallback(async (traceId: string | null) => {
    const requestId = ++traceRequestIdRef.current;

    if (!traceId) {
      setSelectedTraceSpans([]);
      setSelectedTraceError(null);
      setSelectedTraceLoading(false);
      return;
    }

    setSelectedTraceLoading(true);
    setSelectedTraceError(null);

    if (importedData) {
      const spans = importedData.traceMap.get(traceId);
      if (requestId !== traceRequestIdRef.current) {
        return;
      }
      if (!spans) {
        setSelectedTraceSpans([]);
        setSelectedTraceError(`Trace ${traceId} is not present in the imported bundle.`);
        setSelectedTraceLoading(false);
        return;
      }
      setSelectedTraceSpans(spans);
      setSelectedTraceLoading(false);
      return;
    }

    try {
      const spans = await fetchJson<StoredSpan[]>(`/api/traces/${encodeURIComponent(traceId)}/spans`);
      if (requestId !== traceRequestIdRef.current) {
        return;
      }
      setSelectedTraceSpans(spans.map(normalizeSpan));
    } catch (error) {
      if (requestId !== traceRequestIdRef.current) {
        return;
      }
      setSelectedTraceSpans([]);
      setSelectedTraceError((error as Error).message);
    } finally {
      if (requestId === traceRequestIdRef.current) {
        setSelectedTraceLoading(false);
      }
    }
  }, [importedData]);

  useEffect(() => {
    updateHash(viewMode, selectedTraceId);
  }, [selectedTraceId, updateHash, viewMode]);

  useEffect(() => {
    if (typeof window === 'undefined') {
      return;
    }

    const onHashChange = () => {
      const next = parseHash(window.location.hash);
      if (isViewMode(next.view) && next.view !== viewMode) {
        setViewMode(next.view);
      }
      if ((next.traceId ?? null) !== selectedTraceId) {
        setSelectedTraceId(next.traceId ?? null);
      }
    };

    window.addEventListener('hashchange', onHashChange);
    return () => window.removeEventListener('hashchange', onHashChange);
  }, [selectedTraceId, viewMode]);

  useEffect(() => {
    if (importedData) {
      const importedServices = uniq([
        ...importedData.traces.map((trace) => trace.service_name),
        ...importedData.bundle.insights.services.map((service) => service.service)
      ]);
      setServices(importedServices);
      setServicesError(null);
      setInsights(importedData.bundle.insights);
      setInsightsError(null);
      setInsightsLoading(false);
      setExplore(null);
      setExploreError(null);
      setExploreLoading(false);
      return;
    }

    void loadServices();
    void loadInsights(appliedFilters);
  }, [appliedFilters, importedData, loadInsights, loadServices]);

  useEffect(() => {
    if (importedData) {
      return;
    }
    if (viewMode === 'explore') {
      void loadExplore(appliedFilters);
      return;
    }
    ++requests.current.explore;
    setExplore(null);
    setExploreError(null);
    setExploreLoading(false);
  }, [appliedFilters, importedData, loadExplore, viewMode]);

  useEffect(() => {
    void loadSelectedTrace(selectedTraceId);
  }, [importedData, loadSelectedTrace, selectedTraceId, traceReloadKey]);

  useEffect(() => {
    setSelectedTraceIds(new Set());
  }, [appliedFilters, importedData]);

  const applyFilters = useCallback((next: InsightFilters) => {
    const normalized = normalizeFilters(next);
    setAppliedFilters(normalized);
    setDraftFilters(normalized);
    setTimePreset(detectPreset(normalized));
    setAppliedPreset(detectPreset(normalized));
    setGlobalError(null);
    setGlobalInfo(null);
  }, []);

  const onApply = useCallback(() => {
    if (importedData) {
      setGlobalInfo('Imported bundles are read-only. Exit bundle mode to query the live backend again.');
      return;
    }
    const range = timePreset === 'custom' ? draftFilters : getPresetRange(timePreset);
    applyFilters({ ...draftFilters, from: range.from, to: range.to });
  }, [applyFilters, draftFilters, importedData, timePreset]);

  const onReset = useCallback(() => {
    if (importedData) {
      setGlobalInfo('Imported bundles keep their exported filters. Exit bundle mode to reset live filters.');
      return;
    }
    setTimePreset(DEFAULT_PRESET);
    applyFilters(getPresetRange(DEFAULT_PRESET));
  }, [applyFilters, importedData]);

  const onTimePresetChange = useCallback((preset: TimePreset) => {
    setTimePreset(preset);
    if (preset === 'custom') {
      return;
    }
    setDraftFilters((current) => ({ ...current, from: getPresetRange(preset).from, to: getPresetRange(preset).to }));
  }, []);

  const onOpenBucket = useCallback((bucket: InsightBucket) => {
    const nextFilters = normalizeFilters({ ...appliedFilters, from: bucket.from, to: bucket.to });
    setDraftFilters(nextFilters);
    setAppliedFilters(nextFilters);
    setTimePreset('custom');
    setAppliedPreset('custom');
    setViewMode('explore');
    setGlobalInfo(importedData ? 'Showing only imported traces that fall inside the selected bucket.' : null);
  }, [appliedFilters, importedData]);

  const onToggleTrace = useCallback((traceId: string, checked: boolean) => {
    setGlobalError(null);
    setSelectedTraceIds((previous) => {
      const next = new Set(previous);
      if (checked) {
        if (!next.has(traceId) && next.size >= 20) {
          setGlobalError('Investigation export supports at most 20 traces per bundle.');
          return previous;
        }
        next.add(traceId);
      } else {
        next.delete(traceId);
      }
      return next;
    });
  }, []);

  const onExportSelected = useCallback(async (explicitTraceId?: string) => {
    if (importedData) {
      downloadJson(bundleFilename(importedData.bundle), importedData.bundle);
      return;
    }

    const traceIds = explicitTraceId ? [explicitTraceId] : [...selectedTraceIds];
    if (traceIds.length === 0) {
      setGlobalError('Select at least one trace to export an investigation bundle.');
      return;
    }
    if (traceIds.length > 20) {
      setGlobalError('Investigation export supports at most 20 traces per bundle.');
      return;
    }

    setExporting(true);
    setGlobalError(null);
    setGlobalInfo(null);

    try {
      const bundle = await fetchJson<unknown>(buildInvestigationUrl(appliedFilters, { traceIds }));
      const validation = validateInvestigationBundle(bundle);
      if (!validation.ok) {
        throw new Error(validation.error);
      }
      downloadJson(bundleFilename(validation.value), validation.value);
    } catch (error) {
      setGlobalError((error as Error).message);
    } finally {
      setExporting(false);
    }
  }, [appliedFilters, importedData, selectedTraceIds]);

  const onRefresh = useCallback(() => {
    if (importedData) {
      setTraceReloadKey((value) => value + 1);
      setGlobalInfo('Reloaded the selected trace from the imported bundle.' );
      return;
    }
    setTraceReloadKey((value) => value + 1);
    if (appliedPreset !== 'custom') {
      const range = getPresetRange(appliedPreset);
      applyFilters({ ...appliedFilters, from: range.from, to: range.to });
      return;
    }
    void loadServices();
    void loadInsights(appliedFilters);
    if (viewMode === 'explore') {
      void loadExplore(appliedFilters);
    }
  }, [appliedFilters, appliedPreset, applyFilters, importedData, loadExplore, loadInsights, loadServices, viewMode]);

  const onImportClick = useCallback(() => {
    fileInputRef.current?.click();
  }, []);

  const onImportFile = useCallback(async (event: Event) => {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (!file) {
      return;
    }

    try {
      if (file.size > INVESTIGATION_BUNDLE_BYTE_LIMIT) throw new Error('Bundle exceeds 32 MiB.');
      const text = await file.text();
      const parsed = JSON.parse(text) as unknown;
      const validation = validateInvestigationBundle(parsed);
      if (!validation.ok) {
        throw new Error(validation.error);
      }

      liveSnapshotRef.current = {
        filters: appliedFilters,
        draftFilters,
        timePreset,
        appliedPreset,
        viewMode,
        selectedTraceId
      };

      offlineRef.current = true;
      ++requests.current.services;
      ++requests.current.insights;
      ++requests.current.explore;
      ++traceRequestIdRef.current;
      const data = buildImportedBundleData(validation.value);
      const importedFilters = normalizeFilters(validation.value.filters);
      setImportedData(data);
      setAppliedFilters(importedFilters);
      setDraftFilters(importedFilters);
      setTimePreset(detectPreset(importedFilters, importedFilters.to));
      setSelectedTraceId(null);
      setSelectedTraceSpans([]);
      setSelectedTraceError(null);
      setViewMode('overview');
      setGlobalError(null);
      setGlobalInfo(`Imported ${file.name} locally. No network requests will be made while browsing this bundle.`);
    } catch (error) {
      setGlobalError(`Bundle import failed: ${(error as Error).message}`);
    }
  }, [appliedFilters, appliedPreset, draftFilters, selectedTraceId, timePreset, viewMode]);

  const onExitImported = useCallback(() => {
    const snapshot = liveSnapshotRef.current;
    offlineRef.current = false;
    ++traceRequestIdRef.current;
    setImportedData(null);
    setSelectedTraceSpans([]);
    setSelectedTraceError(null);
    setGlobalError(null);
    setGlobalInfo('Exited imported bundle mode. Live backend queries are active again.');

    if (snapshot) {
      setAppliedFilters(snapshot.filters);
      setDraftFilters(snapshot.draftFilters);
      setTimePreset(snapshot.timePreset);
      setAppliedPreset(snapshot.appliedPreset);
      setSelectedTraceId(snapshot.selectedTraceId);
      setViewMode(snapshot.viewMode);
      return;
    }

    setAppliedFilters(DEFAULT_FILTERS);
    setDraftFilters(DEFAULT_FILTERS);
    setTimePreset(DEFAULT_PRESET);
    setSelectedTraceId(null);
    setViewMode('overview');
  }, []);

  const showRefreshBusy = insightsLoading || exploreLoading || selectedTraceLoading || exporting;

  return html`
    <div class="portal-layout">
      <header class="portal-header">
        <button ref=${menuToggleRef} class="portal-header__btn portal-menu-toggle"
          aria-label=${menuOpen ? 'Close navigation' : 'Open navigation'}
          aria-expanded=${menuOpen} aria-controls="main-navigation"
          onClick=${() => setMenuOpen(open => !open)}>☰</button>
        <a href="#" class="portal-header__brand" onClick=${(event: Event) => event.preventDefault()}>
          <span class="portal-header__brand-icon">🔍</span>
          <span>GoTel</span>
        </a>

        <div class="portal-header__search">
          <input
            type="search"
            placeholder="Open trace ID (press Enter)"
            aria-label="Open trace ID"
            value=${traceInput}
            onInput=${(event: Event) => setTraceInput((event.currentTarget as HTMLInputElement).value)}
            onKeyDown=${(event: KeyboardEvent) => {
              if (event.key !== 'Enter') return;
              const id = traceInput.trim().toLowerCase();
              if (!/^[0-9a-f]{32}$/.test(id)) { setGlobalError('Enter a 32-digit hexadecimal trace ID.'); return; }
              setGlobalError(null);
              selectTrace(id);
            }}
          />
        </div>

        <div class="portal-header__actions gotel-header-actions">
          <input
            ref=${fileInputRef}
            class="gotel-file-input"
            type="file"
            accept="application/json,.json"
            onChange=${onImportFile}
          />
          ${imported ? html`
            <button class="fluent-btn fluent-btn--secondary fluent-btn--small" onClick=${() => downloadJson(bundleFilename(importedData.bundle), importedData.bundle)}>
              Download bundle
            </button>
            <button class="fluent-btn fluent-btn--secondary fluent-btn--small" onClick=${onExitImported}>
              Exit import
            </button>
          ` : html`
            <button class="fluent-btn fluent-btn--secondary fluent-btn--small" onClick=${onImportClick}>
              Import bundle
            </button>
          `}
          <button
            class="portal-header__btn ${showRefreshBusy ? 'gotel-btn--loading' : ''}"
            onClick=${onRefresh}
            disabled=${exporting}
            aria-label=${showRefreshBusy ? 'Refreshing data' : 'Refresh data'}
            title="Refresh data"
          >
            ${showRefreshBusy ? '⏳' : '🔄'}
          </button>
        </div>
      </header>

      <div class="portal-body">
        ${menuOpen ? html`<button class="portal-nav-backdrop" aria-label="Close navigation overlay" tabindex="-1" onClick=${closeMenu}></button>` : ''}
        <nav id="main-navigation" class=${`portal-nav ${menuOpen ? 'portal-nav--open' : ''}`} aria-label="Main navigation">
          ${navItems.map((item) => html`
            <button
              class="portal-nav__item ${viewMode === item.id ? 'portal-nav__item--active' : ''}"
              onClick=${() => { setViewMode(item.id); if (menuOpen) closeMenu(); }}
              aria-label=${item.label}
              aria-current=${viewMode === item.id ? 'page' : undefined}
            >
              <span class="portal-nav__icon">${item.icon}</span>
              <span class="portal-nav__label">${item.label}</span>
            </button>
          `)}

          <div class="portal-nav__divider"></div>
          <a class="portal-nav__item" href="https://github.com/rcarmo/gotel/tree/main/docs" target="_blank" rel="noreferrer noopener">
            <span class="portal-nav__icon">📚</span>
            <span class="portal-nav__label">Documentation</span>
          </a>
        </nav>

        <main class="portal-main" id="main-content" tabindex="-1">
          <div class="portal-command-bar">
            <h1 class="portal-command-bar__title">${getViewTitle(viewMode)}</h1>
            ${selectedTraceId ? html`<code class="gotel-trace-chip">#${selectedTraceId}</code>` : ''}
            ${!imported && viewMode === 'timeline' && selectedTraceId ? html`
              <button class="fluent-btn fluent-btn--primary" disabled=${exporting} onClick=${() => onExportSelected(selectedTraceId)}>Export this trace</button>
            ` : ''}
            ${!imported && viewMode === 'explore' ? html`
              <button class="fluent-btn fluent-btn--primary" onClick=${() => onExportSelected()} disabled=${exporting || selectedTraceIds.size === 0}>
                ${exporting ? 'Exporting…' : `Export selected (${selectedTraceIds.size})`}
              </button>
            ` : ''}
          </div>

          <div class="portal-content gotel-view-shell">
            ${imported ? html`
              <div class="fluent-alert fluent-alert--warning">
                <div class="fluent-alert__icon">📦</div>
                <div class="fluent-alert__content">
                  <div class="fluent-alert__title">Imported investigation bundle</div>
                  <div class="fluent-alert__message">Browsing is fully local and read-only. Shared filters reflect the exported bundle and bucket drilldowns only inspect traces included in that bundle.</div>
                </div>
              </div>
            ` : ''}

            ${globalError ? html`
              <div class="fluent-alert fluent-alert--error">
                <div class="fluent-alert__icon">⚠️</div>
                <div class="fluent-alert__content">
                  <div class="fluent-alert__title">Action failed</div>
                  <div class="fluent-alert__message">${globalError}</div>
                </div>
              </div>
            ` : ''}

            ${globalInfo ? html`
              <div class="fluent-alert fluent-alert--info">
                <div class="fluent-alert__icon">ℹ️</div>
                <div class="fluent-alert__content">
                  <div class="fluent-alert__title">Status</div>
                  <div class="fluent-alert__message">${globalInfo}</div>
                </div>
              </div>
            ` : ''}

            ${servicesError ? html`
              <div class="fluent-alert fluent-alert--warning">
                <div class="fluent-alert__icon">⚠️</div>
                <div class="fluent-alert__content">
                  <div class="fluent-alert__title">Service discovery unavailable</div>
                  <div class="fluent-alert__message">${servicesError}</div>
                </div>
              </div>
            ` : ''}

            <${FiltersBar}
              filters=${draftFilters}
              timePreset=${timePreset}
              services=${services}
              disabled=${imported}
              busy=${insightsLoading || exploreLoading}
              onChange=${setDraftFilters}
              onTimePresetChange=${onTimePresetChange}
              onApply=${onApply}
              onReset=${onReset}
            />

            ${viewMode === 'overview' ? html`
              <${OverviewView}
                insights=${insights}
                loading=${insightsLoading}
                errorMessage=${insightsError}
                imported=${imported}
                onSelectTrace=${(traceId: string) => selectTrace(traceId, 'timeline')}
                onOpenBucket=${onOpenBucket}
              />
            ` : viewMode === 'explore' ? html`
              <${ExploreView}
                result=${exploreResult}
                loading=${exploreLoading}
                errorMessage=${exploreError}
                imported=${imported}
                selectedTraceId=${selectedTraceId}
                selectedTraceIds=${selectedTraceIds}
                onSelectTrace=${(traceId: string) => selectTrace(traceId, 'timeline')}
                onToggleTrace=${onToggleTrace}
                onExportSelected=${imported ? undefined : () => onExportSelected()}
                exportDisabled=${exporting || selectedTraceIds.size === 0}
              />
            ` : viewMode === 'timeline' ? html`
              <${TimelineView}
                key=${selectedTraceId ?? 'no-trace'}
                selectedTraceId=${selectedTraceId}
                spans=${selectedTraceSpans}
                loading=${selectedTraceLoading}
                errorMessage=${selectedTraceError}
              />
            ` : viewMode === 'metrics' ? html`
              <${MetricsView}
                insights=${insights}
                loading=${insightsLoading}
                errorMessage=${insightsError}
                onOpenBucket=${onOpenBucket}
              />
            ` : html`
              <${AgentsView}
                insights=${insights}
                loading=${insightsLoading}
                errorMessage=${insightsError}
                onSelectTrace=${(traceId: string) => selectTrace(traceId, 'timeline')}
              />
            `}
          </div>
        </main>
      </div>
    </div>
  `;
}

const appContainer = document.getElementById('app');
if (appContainer) {
  render(h(App, {}), appContainer);
}
