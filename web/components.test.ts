import { describe, it, expect, beforeEach } from 'bun:test';
import { h } from 'preact';
import { render } from 'preact-render-to-string';
import { StatsDashboard } from './components/StatsDashboard';
import { TracesList } from './components/TracesList';
import { TimelineView } from './components/TimelineView';
import { ExceptionsView } from './components/ExceptionsView';
import { MetricsView } from './components/MetricsView';

// Mock data for testing
const mockTraces = [
  {
    trace_id: 'trace-1',
    span_name: 'HTTP GET /api',
    service_name: 'api-service',
    duration_ms: 1500,
    status_code: 0,
    span_count: 5
  },
  {
    trace_id: 'trace-2',
    span_name: 'HTTP POST /users',
    service_name: 'api-service',
    duration_ms: 2500,
    status_code: 2,
    span_count: 8
  },
  {
    trace_id: 'trace-3',
    span_name: 'Database Query',
    service_name: 'db-service',
    duration_ms: 800,
    status_code: 0,
    span_count: 3
  }
];

const mockSpans = [
  {
    trace_id: 'trace-1',
    span_id: 'span-1',
    parent_span_id: null,
    service_name: 'api-service',
    span_name: 'HTTP GET /api',
    start_time: 1000000,
    end_time: 2500000,
    duration_ms: 1500,
    status_code: 0
  },
  {
    trace_id: 'trace-1',
    span_id: 'span-2',
    parent_span_id: 'span-1',
    service_name: 'api-service',
    span_name: 'Parse Request',
    start_time: 1100000,
    end_time: 1300000,
    duration_ms: 200,
    status_code: 0
  }
];

const mockExceptions = [
  {
    trace_id: 'trace-2',
    span_id: 'span-error',
    service_name: 'api-service',
    span_name: 'HTTP POST /users',
    exception_type: 'ValidationError',
    message: 'Invalid user data',
    stack_trace: 'Error: Invalid user data\n    at validateUser...',
    timestamp: Date.now(),
    severity: 'critical'
  }
];

describe('Component Rendering', () => {
  describe('StatsDashboard', () => {
    it('should render without crashing', () => {
      const result = render(h(StatsDashboard, {
        traces: mockTraces,
        spans: mockSpans,
        exceptions: mockExceptions
      }));
      
      expect(result).toBeString();
      expect(result).toContain('fluent-metric-tile');
      expect(result).toContain('Total Traces');
      expect(result).toContain('Total Spans');
      expect(result).toContain('Error Spans');
      expect(result).toContain('Exceptions');
    });

    it('should display correct metrics', () => {
      const result = render(h(StatsDashboard, {
        traces: mockTraces,
        spans: mockSpans,
        exceptions: mockExceptions
      }));
      
      expect(result).toContain('3'); // Total traces
      expect(result).toContain('2'); // Total spans
      expect(result).toContain('0'); // Error spans (none in mockSpans)
      expect(result).toContain('1'); // Exceptions
    });

    it('should handle empty data gracefully', () => {
      const result = render(h(StatsDashboard, {
        traces: [],
        spans: [],
        exceptions: []
      }));
      
      expect(result).toContain('0'); // Should show 0 for all metrics
    });
  });

  describe('TracesList', () => {
    it('should render without crashing', () => {
      const result = render(h(TracesList, {
        traces: mockTraces,
        spans: mockSpans,
        selectedTraceId: null,
        onTraceSelect: () => {},
        onViewModeChange: () => {},
        currentViewMode: 'traces'
      }));
      
      expect(result).toBeString();
      expect(result).toContain('Traces by Service');
      expect(result).toContain('api-service');
      expect(result).toContain('db-service');
    });

    it('should show service grouping', () => {
      const result = render(h(TracesList, {
        traces: mockTraces,
        spans: mockSpans,
        selectedTraceId: null,
        onTraceSelect: () => {},
        onViewModeChange: () => {},
        currentViewMode: 'traces'
      }));
      
      expect(result).toContain('api-service (2)');
      expect(result).toContain('db-service (1)');
    });

    it('should highlight selected trace', () => {
      const result = render(h(TracesList, {
        traces: mockTraces,
        spans: mockSpans,
        selectedTraceId: 'trace-1',
        onTraceSelect: () => {},
        onViewModeChange: () => {},
        currentViewMode: 'traces'
      }));
      
      expect(result).toContain('trace-1');
    });

    it('should show error indicator for failed traces', () => {
      const result = render(h(TracesList, {
        traces: mockTraces,
        spans: mockSpans,
        selectedTraceId: null,
        onTraceSelect: () => {},
        onViewModeChange: () => {},
        currentViewMode: 'traces'
      }));
      
      expect(result).toContain('fluent-badge--tint-error');
    });
  });

  describe('TimelineView', () => {
    it('should render without crashing', () => {
      const result = render(h(TimelineView, {
        selectedTraceId: 'trace-1',
        spans: mockSpans
      }));
      
      expect(result).toBeString();
    });

    it('should handle empty spans gracefully', () => {
      const result = render(h(TimelineView, {
        selectedTraceId: 'trace-1',
        spans: []
      }));
      
      expect(result).toBeString();
    });
  });

  describe('ExceptionsView', () => {
    it('should render without crashing', () => {
      const result = render(h(ExceptionsView, {
        exceptions: mockExceptions
      }));
      
      expect(result).toBeString();
      expect(result).toContain('fluent-card');
      expect(result).toContain('ValidationError');
    });

    it('should handle empty exceptions gracefully', () => {
      const result = render(h(ExceptionsView, {
        exceptions: []
      }));
      
      expect(result).toBeString();
    });
  });

  describe('MetricsView', () => {
    it('should render without crashing', () => {
      const result = render(h(MetricsView, {
        spans: mockSpans,
        viewMode: 'metrics'
      }));
      
      expect(result).toBeString();
    });

    it('should handle empty spans gracefully', () => {
      const result = render(h(MetricsView, {
        spans: [],
        viewMode: 'metrics'
      }));
      
      expect(result).toBeString();
    });
  });
});

describe('Component Behavior', () => {
  describe('StatsDashboard', () => {
    it('should calculate error rate correctly', () => {
      const spansWithErrors = [
        ...mockSpans,
        {
          trace_id: 'trace-error',
          span_id: 'error-span',
          service_name: 'api-service',
          span_name: 'Failed Operation',
          start_time: 1000000,
          end_time: 1500000,
          duration_ms: 500,
          status_code: 2 // Error status
        }
      ];
      
      const result = render(h(StatsDashboard, {
        traces: mockTraces,
        spans: spansWithErrors,
        exceptions: mockExceptions
      }));
      
      // Should show 1 error span out of 3 total
      expect(result).toContain('1'); // Error spans
      expect(result).toContain('33.3%'); // Error rate (1/3)
    });
  });

  describe('TracesList', () => {
    it('should group traces by service correctly', () => {
      const result = render(h(TracesList, {
        traces: mockTraces,
        spans: mockSpans,
        selectedTraceId: null,
        onTraceSelect: () => {},
        onViewModeChange: () => {},
        currentViewMode: 'traces'
      }));
      
      // Should show 2 traces for api-service and 1 for db-service
      expect(result).toContain('api-service (2)');
      expect(result).toContain('db-service (1)');
    });
  });
});