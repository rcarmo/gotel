// Utility functions for data processing
import type { Span } from './state';

// Calculate percentile from array of numbers
export function calculatePercentile(values: number[], percentile: number): number | undefined {
  if (values.length === 0) return undefined;
  const sorted = [...values].sort((a, b) => a - b);
  const index = Math.ceil((percentile / 100) * sorted.length) - 1;
  return sorted[Math.min(Math.max(0, index), sorted.length - 1)];
}

// Build hierarchical span tree for timeline rendering
type SpanWithChildren = Span & { children: SpanWithChildren[] };
type SpanWithLevel = Span & { level: number, children: SpanWithChildren[] };

export function buildSpanHierarchy(spans: Span[]): SpanWithLevel[] {
  const spanMap = new Map<string, SpanWithChildren>();
  const root: SpanWithChildren[] = [];
  
  // Create span map
  spans.forEach(span => {
    spanMap.set(span.span_id, { ...span, children: [] });
  });
  
  // Build hierarchy
  spans.forEach(span => {
    const currentSpan = spanMap.get(span.span_id)!;
    if (span.parent_span_id && spanMap.has(span.parent_span_id)) {
      spanMap.get(span.parent_span_id)!.children.push(currentSpan);
    } else {
      root.push(currentSpan);
    }
  });
  
  // Flatten hierarchy for rendering
  const flatten = (nodes: SpanWithChildren[], level = 0): SpanWithLevel[] => {
    const result: SpanWithLevel[] = [];
    nodes.forEach(node => {
      result.push({ ...node, level });
      if (node.children.length > 0) {
        result.push(...flatten(node.children, level + 1));
      }
    });
    return result;
  };
  
  return flatten(root);
}

// Escape HTML special characters to prevent XSS
function escapeHtml(str: string): string {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

// Show span details modal
export function showSpanDetails(span: Span) {
  const modal = document.createElement('div');
  modal.style.cssText = `
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0,0,0,0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  `;
  
  const statusLabel = span.status_code === 2 ? 'ERROR' : span.status_code === 1 ? 'WARNING' : 'OK';
  const statusColor = span.status_code === 2 ? '#e74c3c' : span.status_code === 1 ? '#f39c12' : '#27ae60';
  
  modal.innerHTML = `
    <div style="
      background: white;
      padding: 30px;
      border-radius: 8px;
      max-width: 800px;
      max-height: 80vh;
      overflow-y: auto;
      box-shadow: 0 4px 20px rgba(0,0,0,0.3);
    ">
      <h2 style="margin-top: 0; color: #2c3e50;">🔍 Span Details</h2>
      
      <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 20px; margin-bottom: 20px;">
        <div>
          <strong>Trace ID:</strong><br>
          <code style="background: #f4f4f4; padding: 4px; border-radius: 3px; word-break: break-all;">${escapeHtml(span.trace_id)}</code>
        </div>
        <div>
          <strong>Span ID:</strong><br>
          <code style="background: #f4f4f4; padding: 4px; border-radius: 3px; word-break: break-all;">${escapeHtml(span.span_id)}</code>
        </div>
        <div>
          <strong>Parent Span ID:</strong><br>
          <code style="background: #f4f4f4; padding: 4px; border-radius: 3px; word-break: break-all;">${escapeHtml(span.parent_span_id || 'None (Root)')}</code>
        </div>
        <div>
          <strong>Status:</strong><br>
          <span style="
            padding: 4px 8px;
            border-radius: 12px;
            background: ${statusColor};
            color: white;
            font-size: 12px;
          ">${statusLabel}</span>
        </div>
      </div>
      
      <div style="margin-bottom: 20px;">
        <strong>Service Name:</strong> ${escapeHtml(span.service_name)}<br>
        <strong>Span Name:</strong> ${escapeHtml(span.span_name)}<br>
        <strong>Duration:</strong> ${(span.duration_ms || 0).toFixed(3)}ms<br>
        <strong>Start Time:</strong> ${escapeHtml(new Date(span.start_time / 1000000).toISOString())}<br>
        <strong>End Time:</strong> ${escapeHtml(new Date(span.end_time / 1000000).toISOString())}
      </div>
      
      <div style="margin-bottom: 20px;">
        <h3>📊 Full Span Data</h3>
        <pre style="
          background: #f8f9fa;
          padding: 15px;
          border-radius: 4px;
          overflow-x: auto;
          font-size: 12px;
          max-height: 300px;
          border: 1px solid #dee2e6;
        ">${escapeHtml(JSON.stringify(span, null, 2))}</pre>
      </div>
      
      <div style="text-align: right;">
        <button onclick="this.closest('div').parentElement.remove()" style="
          padding: 8px 16px;
          background: #3498db;
          color: white;
          border: none;
          border-radius: 4px;
          cursor: pointer;
        ">Close</button>
      </div>
    </div>
  `;
  
  document.body.appendChild(modal);
  modal.addEventListener('click', (e) => {
    if (e.target === modal) {
      modal.remove();
    }
  });
}

// Format duration in human readable format
export function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms.toFixed(1)}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

// Get status color based on span status
export function getStatusColor(statusCode: number): string {
  switch (statusCode) {
    case 0: return '#27ae60'; // OK
    case 1: return '#f39c12'; // WARNING  
    case 2: return '#e74c3c'; // ERROR
    default: return '#95a5a6'; // UNKNOWN
  }
}

// Get status text
export function getStatusText(statusCode: number): string {
  switch (statusCode) {
    case 0: return 'OK';
    case 1: return 'WARNING';
    case 2: return 'ERROR';
    default: return 'UNKNOWN';
  }
}