import { h, render } from 'preact';
import { html } from 'htm/preact';
import type { ViewMode, TraceSummary, Exception, Span } from './state';
import { StatsDashboard } from './components/StatsDashboard';
import { TracesList } from './components/TracesList';
import { TimelineView } from './components/TimelineView';
import { ExceptionsView } from './components/ExceptionsView';
import { MetricsView } from './components/MetricsView';
import { showSpanDetails } from './utils';

// Import state management
import {
  getState,
  setCurrentTraces,
  setCurrentSpans,
  setCurrentExceptions,
  setLoading,
  setSelectedTraceId,
  setViewMode
} from './state';

// Navigation items for Azure-style sidebar
const navItems = [
  { id: 'traces', icon: '📊', label: 'Trace Explorer' },
  { id: 'timeline', icon: '⏱️', label: 'Timeline' },
  { id: 'exceptions', icon: '⚠️', label: 'Exceptions' },
  { id: 'metrics', icon: '📈', label: 'Metrics' },
];

// Fetch data from API
async function fetchTraces() {
  setLoading(true);
  try {
    const [tracesResp, spansResp, exceptionsResp] = await Promise.all([
      fetch('/api/traces'),
      fetch('/api/spans'), 
      fetch('/api/exceptions')
    ]);
    
    if (tracesResp.ok && spansResp.ok) {
      const traces: TraceSummary[] = await tracesResp.json();
      const spans: Span[] = await spansResp.json();
      const exceptions: Exception[] = exceptionsResp.ok ? await exceptionsResp.json() : [];
      
      setCurrentTraces(traces);
      setCurrentSpans(spans);
      setCurrentExceptions(exceptions);
      
      renderApp();
    }
  } catch (error) {
    console.error('Error fetching traces:', error);
  } finally {
    setLoading(false);
    renderApp();
  }
}

// Get view title for breadcrumb
function getViewTitle(viewMode: ViewMode): string {
  switch (viewMode) {
    case 'traces': return 'Trace Explorer';
    case 'timeline': return 'Timeline View';
    case 'exceptions': return 'Exceptions';
    case 'metrics': return 'Metrics';
    default: return 'Dashboard';
  }
}

// Main App component - Azure Portal Layout
function App() {
  const { currentTraces, currentSpans, currentExceptions, loading, selectedTraceId, viewMode } = getState();
  
  return html`
    <div class="portal-layout">
      <!-- Header / Command Bar -->
      <header class="portal-header">
        <a href="/" class="portal-header__brand">
          <span class="portal-header__brand-icon">🔍</span>
          <span>GoTel</span>
        </a>
        
        <div class="portal-header__search">
          <input 
            type="search" 
            placeholder="Search traces, spans, services..." 
            aria-label="Search"
          />
        </div>
        
        <div class="portal-header__actions">
          <button 
            class="portal-header__btn ${loading ? 'gotel-btn--loading' : ''}"
            onClick=${fetchTraces}
            disabled=${loading}
            aria-label="${loading ? 'Loading...' : 'Refresh data'}"
            title="Refresh data"
          >
            ${loading ? '⏳' : '🔄'}
          </button>
          <button class="portal-header__btn" title="Notifications">
            🔔
          </button>
          <button class="portal-header__btn" title="Settings">
            ⚙️
          </button>
        </div>
      </header>
      
      <div class="portal-body">
        <!-- Left Navigation -->
        <nav class="portal-nav" aria-label="Main navigation">
          ${navItems.map(item => html`
            <button 
              class="portal-nav__item ${viewMode === item.id ? 'portal-nav__item--active' : ''}"
              onClick=${() => { setViewMode(item.id as ViewMode); renderApp(); }}
              aria-label="${item.label}"
              aria-current=${viewMode === item.id ? 'page' : undefined}
            >
              <span class="portal-nav__icon">${item.icon}</span>
              <span class="portal-nav__label">${item.label}</span>
              ${item.id === 'exceptions' && currentExceptions.length > 0 ? html`
                <span class="fluent-tab__count" style="margin-left: auto;">${currentExceptions.length}</span>
              ` : ''}
            </button>
          `)}
          
          <div class="portal-nav__divider"></div>
          
          <button class="portal-nav__item" title="Documentation">
            <span class="portal-nav__icon">📚</span>
            <span class="portal-nav__label">Documentation</span>
          </button>
        </nav>
        
        <!-- Main Content -->
        <main class="portal-main" id="main-content" tabindex="-1">
          <!-- Breadcrumb -->
          <div class="portal-breadcrumb">
            <a href="/" class="portal-breadcrumb__item">GoTel</a>
            <span class="portal-breadcrumb__separator">›</span>
            <span class="portal-breadcrumb__current">${getViewTitle(viewMode)}</span>
          </div>
          
          <!-- Command Bar -->
          <div class="portal-command-bar">
            <h1 class="portal-command-bar__title">${getViewTitle(viewMode)}</h1>
            <button 
              class="fluent-btn fluent-btn--primary"
              onClick=${fetchTraces}
              disabled=${loading}
            >
              ${loading ? '⏳ Loading...' : '🔄 Refresh'}
            </button>
          </div>
          
          <!-- Content Area -->
          <div class="portal-content">
            <!-- Statistics Dashboard (always visible) -->
            <${StatsDashboard} 
              traces=${currentTraces} 
              spans=${currentSpans} 
              exceptions=${currentExceptions}
            />
            
            <!-- View Content -->
            <div class="fluent-card" style="margin-top: var(--space-xl);">
              ${viewMode === 'traces' ? html`
                <${TracesList} 
                  traces=${currentTraces}
                  spans=${currentSpans}
                  selectedTraceId=${selectedTraceId}
                  onTraceSelect=${(traceId: string) => { setSelectedTraceId(traceId); renderApp(); }}
                  onViewModeChange=${(mode: ViewMode) => { setViewMode(mode); renderApp(); }}
                  currentViewMode=${viewMode}
                />
              ` : 
                viewMode === 'timeline' ? html`
                  <${TimelineView} 
                    selectedTraceId=${selectedTraceId}
                    spans=${currentSpans}
                  />
                ` :
                viewMode === 'exceptions' ? html`
                  <${ExceptionsView} 
                    exceptions=${currentExceptions}
                  />
                ` :
                html`
                  <${MetricsView} 
                    spans=${currentSpans}
                    viewMode=${viewMode}
                  />
                `}
            </div>
          </div>
        </main>
      </div>
    </div>
  `;
}

// Render function
function renderApp() {
  const appContainer = document.getElementById('app');
  if (appContainer) {
    render(h(App, {}), appContainer);
  }
}

// Initial render
renderApp();

// Auto-load data on page load
setTimeout(fetchTraces, 100);