package sqliteexporter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"

	"github.com/gotel/storage/sqlite"
)

func TestNewSQLiteExporter(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	cfg := &Config{
		DBPath:      "test.db",
		Prefix:      "otel",
		SendMetrics: true,
		StoreTraces: true,
	}

	exp, err := newSQLiteExporter(cfg, logger)
	if err != nil {
		t.Fatalf("newSQLiteExporter() error = %v", err)
	}
	if exp == nil {
		t.Fatal("newSQLiteExporter() returned nil")
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
	}{
		{
			name:   "empty config gets defaults",
			config: &Config{},
		},
		{
			name: "custom config",
			config: &Config{
				DBPath:    "custom.db",
				Prefix:    "custom",
				Retention: 24 * time.Hour,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if err != nil {
				t.Errorf("Validate() error = %v", err)
			}
			if tt.config.DBPath == "" {
				t.Error("DBPath should have default")
			}
			if tt.config.Prefix == "" {
				t.Error("Prefix should have default")
			}
			if tt.config.Retention == 0 {
				t.Error("Retention should have default")
			}
		})
	}
}

func TestPushTraces(t *testing.T) {
	exp := newTestExporter(t)
	defer exp.shutdown(context.Background())

	ctx := context.Background()

	// Create test trace data
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "test-service")

	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span.SetName("test-operation")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	// Push traces
	err := exp.pushTraces(ctx, td)
	if err != nil {
		t.Fatalf("pushTraces() error = %v", err)
	}

	// Verify data was stored
	stats, err := exp.store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.SpanCount != 1 {
		t.Errorf("Expected 1 span, got %d", stats.SpanCount)
	}
	if stats.MetricCount < 1 {
		t.Errorf("Expected at least 1 metric, got %d", stats.MetricCount)
	}
}

func TestPushTracesWithError(t *testing.T) {
	exp := newTestExporter(t)
	defer exp.shutdown(context.Background())

	ctx := context.Background()

	// Create trace with error span
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "test-service")

	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span.SetName("failing-operation")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	span.Status().SetCode(ptrace.StatusCodeError)

	err := exp.pushTraces(ctx, td)
	if err != nil {
		t.Fatalf("pushTraces() error = %v", err)
	}

	// Verify error_count metric was generated
	stats, _ := exp.store.Stats(ctx)
	if stats.MetricCount < 2 { // span_count, duration_ms, error_count
		t.Errorf("Expected at least 2 metrics (including error_count), got %d", stats.MetricCount)
	}
}

func TestSendMetricsDisabled(t *testing.T) {
	tmpFile, _ := os.CreateTemp("", "gotel-test-*.db")
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	logger, _ := zap.NewDevelopment()
	cfg := &Config{
		DBPath:      tmpFile.Name(),
		Prefix:      "otel",
		SendMetrics: false,
		StoreTraces: true,
	}
	cfg.Validate()

	exp, _ := newSQLiteExporter(cfg, logger)
	exp.start(context.Background(), nil)
	defer exp.shutdown(context.Background())

	ctx := context.Background()

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "test-service")

	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span.SetName("test-op")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	exp.pushTraces(ctx, td)

	stats, _ := exp.store.Stats(ctx)
	if stats.SpanCount != 1 {
		t.Errorf("Expected 1 span, got %d", stats.SpanCount)
	}
	if stats.MetricCount != 0 {
		t.Errorf("Expected 0 metrics with SendMetrics=false, got %d", stats.MetricCount)
	}
}

func TestStoreTracesDisabled(t *testing.T) {
	tmpFile, _ := os.CreateTemp("", "gotel-test-*.db")
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	logger, _ := zap.NewDevelopment()
	cfg := &Config{
		DBPath:      tmpFile.Name(),
		Prefix:      "otel",
		SendMetrics: true,
		StoreTraces: false,
	}
	cfg.Validate()

	exp, _ := newSQLiteExporter(cfg, logger)
	exp.start(context.Background(), nil)
	defer exp.shutdown(context.Background())

	ctx := context.Background()

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "test-service")

	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span.SetName("test-op")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	exp.pushTraces(ctx, td)

	stats, _ := exp.store.Stats(ctx)
	if stats.SpanCount != 0 {
		t.Errorf("Expected 0 spans with StoreTraces=false, got %d", stats.SpanCount)
	}
	if stats.MetricCount == 0 {
		t.Error("Expected metrics to be stored")
	}
}

func TestServiceNamePreservedForStorage(t *testing.T) {
	exp := newTestExporter(t)
	defer exp.shutdown(context.Background())

	ctx := context.Background()
	traceID := pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "checkout API/v1")

	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetTraceID(traceID)
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span.SetName("GET /cart/items")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	if err := exp.pushTraces(ctx, td); err != nil {
		t.Fatalf("pushTraces() error = %v", err)
	}

	spans, err := exp.store.QueryTraceByID(ctx, traceID.String())
	if err != nil {
		t.Fatalf("QueryTraceByID() error = %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}

	var stored map[string]interface{}
	json.Unmarshal(spans[0], &stored)
	if stored["service_name"] != "checkout API/v1" {
		t.Errorf("Expected raw service name preserved, got %v", stored["service_name"])
	}

	metricName := "otel.checkout_API_v1.GET__cart_items.span_count"
	metrics, err := exp.store.QueryMetrics(ctx, sqlite.MetricQueryOptions{Name: metricName})
	if err != nil {
		t.Fatalf("QueryMetrics() error = %v", err)
	}
	if len(metrics) == 0 {
		t.Fatalf("Expected metrics for sanitized service name, got 0")
	}

	var tags map[string]string
	if err := json.Unmarshal([]byte(metrics[0].Tags), &tags); err != nil {
		t.Fatalf("failed to unmarshal metric tags: %v", err)
	}
	if tags["service"] != "checkout API/v1" {
		t.Errorf("Expected raw service tag, got %v", tags["service"])
	}
	if tags["span"] != "GET /cart/items" {
		t.Errorf("Expected raw span tag, got %v", tags["span"])
	}
}

func TestBuildPrefix(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		serviceName string
		spanName    string
		expected    string
	}{
		{
			name:        "basic prefix",
			config:      &Config{Prefix: "otel"},
			serviceName: "myservice",
			spanName:    "myspan",
			expected:    "otel.myservice.myspan",
		},
		{
			name:        "with namespace",
			config:      &Config{Prefix: "otel", Namespace: "prod"},
			serviceName: "myservice",
			spanName:    "myspan",
			expected:    "otel.prod.myservice.myspan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &sqliteExporter{config: tt.config}
			result := e.buildPrefix(tt.serviceName, tt.spanName)
			if result != tt.expected {
				t.Errorf("buildPrefix() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSanitizeMetricName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with space", "with_space"},
		{"with/slash", "with_slash"},
		{"with:colon", "with_colon"},
		{"complex (name) [test]", "complex__name___test_"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeMetricName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeMetricName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestQueryEndpoints(t *testing.T) {
	exp := newTestExporter(t)
	defer exp.shutdown(context.Background())

	ctx := context.Background()

	// Insert test data
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "api-service")

	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span.SetName("GET /users")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	exp.pushTraces(ctx, td)

	// Test /api/services
	t.Run("list services", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/services", nil)
		w := httptest.NewRecorder()
		exp.handleListServices(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var services []string
		json.Unmarshal(w.Body.Bytes(), &services)
		if len(services) != 1 || services[0] != "api-service" {
			t.Errorf("Unexpected services: %v", services)
		}
	})

	// Test /api/traces/{id}
	t.Run("get trace", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/traces/0102030405060708090a0b0c0d0e0f10", nil)
		w := httptest.NewRecorder()
		exp.handleGetTrace(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})

	// Test /api/status
	t.Run("status", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/status", nil)
		w := httptest.NewRecorder()
		exp.handleStatus(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var stats map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &stats)
		if stats["span_count"].(float64) != 1 {
			t.Errorf("Expected span_count=1, got %v", stats["span_count"])
		}
	})

	// Test /ready
	t.Run("ready", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/ready", nil)
		w := httptest.NewRecorder()
		exp.handleReady(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})
}

func TestRenderMetrics(t *testing.T) {
	exp := newTestExporter(t)
	defer exp.shutdown(context.Background())

	ctx := context.Background()

	// Insert test data
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "render-service")

	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span.SetName("render-op")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	exp.pushTraces(ctx, td)

	req := httptest.NewRequest("GET", "/render?target=otel.render-service.render-op.span_count", nil)
	w := httptest.NewRecorder()
	exp.handleRenderMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var result []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &result)
	if len(result) == 0 {
		t.Error("Expected at least one metric series")
	}
}

func TestFindMetrics(t *testing.T) {
	exp := newTestExporter(t)
	defer exp.shutdown(context.Background())

	ctx := context.Background()

	// Insert test data with multiple metrics
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "find-service")

	ss := rs.ScopeSpans().AppendEmpty()
	for i := 0; i < 3; i++ {
		span := ss.Spans().AppendEmpty()
		span.SetTraceID(pcommon.TraceID([16]byte{byte(i + 1), 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
		span.SetSpanID(pcommon.SpanID([8]byte{byte(i + 1), 2, 3, 4, 5, 6, 7, 8}))
		span.SetName("find-op-" + string(rune('a'+i)))
		span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
		span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	}

	exp.pushTraces(ctx, td)

	req := httptest.NewRequest("GET", "/metrics/find?query=otel.find-service.*", nil)
	w := httptest.NewRecorder()
	exp.handleFindMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var result []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &result)
	if len(result) < 3 {
		t.Errorf("Expected at least 3 metric paths, got %d", len(result))
	}
}

func TestFindMetricsGraphiteEscaping(t *testing.T) {
	exp := newTestExporter(t)
	defer exp.shutdown(context.Background())

	ctx := context.Background()
	now := time.Now().Unix()

	exp.store.InsertMetric(ctx, "otel.foo_bar.metric", 1, now, nil)
	exp.store.InsertMetric(ctx, "otel.foXbar.metric", 1, now, nil)
	exp.store.InsertMetric(ctx, "otel.service.operation.metric", 1, now, nil)
	exp.store.InsertMetric(ctx, "otel.service.operZtion.metric", 1, now, nil)
	exp.store.InsertMetric(ctx, "otel.service.operXXtion.metric", 1, now, nil)

	assertQuery := func(pattern string, expected []string) {
		t.Helper()
		req := httptest.NewRequest("GET", "/metrics/find?query="+url.QueryEscape(pattern), nil)
		w := httptest.NewRecorder()
		exp.handleFindMetrics(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", w.Code)
		}

		var resp []map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		if len(resp) != len(expected) {
			t.Fatalf("pattern %q expected %d results, got %d", pattern, len(expected), len(resp))
		}
		seen := make(map[string]bool)
		for _, entry := range resp {
			name, _ := entry["text"].(string)
			seen[name] = true
		}
		for _, want := range expected {
			if !seen[want] {
				t.Fatalf("pattern %q missing result %q", pattern, want)
			}
		}
	}

	assertQuery("otel.foo_bar.*", []string{"otel.foo_bar.metric"})
	assertQuery("otel.service.oper?tion.metric", []string{"otel.service.operation.metric", "otel.service.operZtion.metric"})
}

func TestSearchTraces(t *testing.T) {
	exp := newTestExporter(t)
	defer exp.shutdown(context.Background())

	ctx := context.Background()

	// Insert multiple traces
	for i := 0; i < 3; i++ {
		td := ptrace.NewTraces()
		rs := td.ResourceSpans().AppendEmpty()
		rs.Resource().Attributes().PutStr("service.name", "search-service")

		ss := rs.ScopeSpans().AppendEmpty()
		span := ss.Spans().AppendEmpty()
		span.SetTraceID(pcommon.TraceID([16]byte{byte(i + 1), 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
		span.SetSpanID(pcommon.SpanID([8]byte{byte(i + 1), 2, 3, 4, 5, 6, 7, 8}))
		span.SetName("search-operation")
		span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
		span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))
		exp.pushTraces(ctx, td)
	}

	// Search by service
	req := httptest.NewRequest("GET", "/api/search?service=search-service", nil)
	w := httptest.NewRecorder()
	exp.handleSearchTraces(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var result map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &result)
	traces, ok := result["traces"].([]interface{})
	if !ok || len(traces) < 3 {
		t.Errorf("Expected at least 3 traces, got %v", result)
	}

	// Search by operation
	req = httptest.NewRequest("GET", "/api/search?service=search-service&operation=search-operation", nil)
	w = httptest.NewRecorder()
	exp.handleSearchTraces(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestGetTraceEmpty(t *testing.T) {
	exp := newTestExporter(t)
	defer exp.shutdown(context.Background())

	req := httptest.NewRequest("GET", "/api/traces/", nil)
	w := httptest.NewRecorder()
	exp.handleGetTrace(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for empty trace ID, got %d", w.Code)
	}
}

func TestMultipleSpansPerTrace(t *testing.T) {
	exp := newTestExporter(t)
	defer exp.shutdown(context.Background())

	ctx := context.Background()

	// Create trace with parent-child spans
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "multi-span-service")

	ss := rs.ScopeSpans().AppendEmpty()

	traceID := pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})

	// Parent span
	parentSpan := ss.Spans().AppendEmpty()
	parentSpan.SetTraceID(traceID)
	parentSpan.SetSpanID(pcommon.SpanID([8]byte{1, 0, 0, 0, 0, 0, 0, 0}))
	parentSpan.SetName("parent-op")
	parentSpan.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-200 * time.Millisecond)))
	parentSpan.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	// Child spans
	for i := 0; i < 3; i++ {
		childSpan := ss.Spans().AppendEmpty()
		childSpan.SetTraceID(traceID)
		childSpan.SetSpanID(pcommon.SpanID([8]byte{byte(i + 2), 0, 0, 0, 0, 0, 0, 0}))
		childSpan.SetParentSpanID(pcommon.SpanID([8]byte{1, 0, 0, 0, 0, 0, 0, 0}))
		childSpan.SetName("child-op-" + string(rune('a'+i)))
		childSpan.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
		childSpan.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-50 * time.Millisecond)))
	}

	exp.pushTraces(ctx, td)

	// Verify all spans stored
	stats, _ := exp.store.Stats(ctx)
	if stats.SpanCount != 4 {
		t.Errorf("Expected 4 spans (1 parent + 3 children), got %d", stats.SpanCount)
	}

	// Query by trace ID
	req := httptest.NewRequest("GET", "/api/traces/0102030405060708090a0b0c0d0e0f10", nil)
	w := httptest.NewRecorder()
	exp.handleGetTrace(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var result map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &result)
	resourceSpansAny, ok := result["resourceSpans"].([]interface{})
	if !ok || len(resourceSpansAny) == 0 {
		t.Fatalf("Expected non-empty resourceSpans, got %T (%v)", result["resourceSpans"], result["resourceSpans"])
	}
}

func TestSpanWithAttributes(t *testing.T) {
	exp := newTestExporter(t)
	defer exp.shutdown(context.Background())

	ctx := context.Background()

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "attr-service")

	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span.SetName("attr-operation")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	// Add various attributes
	span.Attributes().PutStr("http.method", "GET")
	span.Attributes().PutStr("http.url", "http://example.com/api/users")
	span.Attributes().PutInt("http.status_code", 200)
	span.Attributes().PutBool("error", false)

	err := exp.pushTraces(ctx, td)
	if err != nil {
		t.Fatalf("pushTraces() error = %v", err)
	}

	// Verify span was stored with attributes
	spans, _ := exp.store.QueryTraceByID(ctx, "0102030405060708090a0b0c0d0e0f10")
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}

	var spanData map[string]interface{}
	json.Unmarshal(spans[0], &spanData)
	attrs, ok := spanData["attributes"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected attributes in span data")
	}
	if attrs["http.method"] != "GET" {
		t.Errorf("Expected http.method=GET, got %v", attrs["http.method"])
	}
}

func TestSpanWithEvents(t *testing.T) {
	exp := newTestExporter(t)
	defer exp.shutdown(context.Background())

	ctx := context.Background()

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "event-service")

	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span.SetName("event-operation")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	// Add events
	event := span.Events().AppendEmpty()
	event.SetName("exception")
	event.SetTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-50 * time.Millisecond)))

	err := exp.pushTraces(ctx, td)
	if err != nil {
		t.Fatalf("pushTraces() error = %v", err)
	}

	// Verify span was stored with events
	spans, _ := exp.store.QueryTraceByID(ctx, "0102030405060708090a0b0c0d0e0f10")
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}

	var spanData map[string]interface{}
	json.Unmarshal(spans[0], &spanData)
	events, ok := spanData["events"].([]interface{})
	if !ok || len(events) == 0 {
		t.Error("Expected events in span data")
	}
}

func TestSpanEventAttributesPreserved(t *testing.T) {
	exp := newTestExporter(t)
	defer exp.shutdown(context.Background())

	ctx := context.Background()

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "event-attrs")

	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span.SetName("event-attrs-op")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	event := span.Events().AppendEmpty()
	event.SetName("exception")
	event.Attributes().PutStr("exception.message", "kaboom")
	event.Attributes().PutInt("exception.code", 500)
	event.SetTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-50 * time.Millisecond)))

	if err := exp.pushTraces(ctx, td); err != nil {
		t.Fatalf("pushTraces() error = %v", err)
	}

	spans, err := exp.store.QueryTraceByID(ctx, "0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatalf("QueryTraceByID() error = %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}

	var spanData map[string]interface{}
	json.Unmarshal(spans[0], &spanData)
	events, ok := spanData["events"].([]interface{})
	if !ok || len(events) == 0 {
		t.Fatalf("Expected events with attributes")
	}

	firstEvent, ok := events[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected event map, got %T", events[0])
	}
	attrs, ok := firstEvent["attributes"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected event attributes to be present")
	}
	if attrs["exception.message"] != "kaboom" {
		t.Errorf("Expected exception message preserved, got %v", attrs["exception.message"])
	}
	if code, ok := attrs["exception.code"].(float64); !ok || int(code) != 500 {
		t.Errorf("Expected exception code 500, got %v", attrs["exception.code"])
	}
}

func TestNamespaceInPrefix(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		service   string
		span      string
		expected  string
	}{
		{
			name:      "no namespace",
			namespace: "",
			service:   "api",
			span:      "get_users",
			expected:  "otel.api.get_users",
		},
		{
			name:      "with namespace",
			namespace: "production",
			service:   "api",
			span:      "get_users",
			expected:  "otel.production.api.get_users",
		},
		{
			name:      "with env namespace",
			namespace: "staging",
			service:   "gateway",
			span:      "POST_orders",
			expected:  "otel.staging.gateway.POST_orders",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Prefix:    "otel",
				Namespace: tt.namespace,
			}
			exp := &sqliteExporter{config: cfg}

			result := exp.buildPrefix(tt.service, tt.span)
			if result != tt.expected {
				t.Errorf("buildPrefix() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func newTestExporter(t *testing.T) *sqliteExporter {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "gotel-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })
	tmpFile.Close()

	logger, _ := zap.NewDevelopment()
	cfg := &Config{
		DBPath:      tmpFile.Name(),
		Prefix:      "otel",
		SendMetrics: true,
		StoreTraces: true,
		QueryPort:   0, // Disable HTTP server in tests
	}
	cfg.Validate()

	exp, err := newSQLiteExporter(cfg, logger)
	if err != nil {
		t.Fatalf("newSQLiteExporter() error = %v", err)
	}

	if err := exp.start(context.Background(), nil); err != nil {
		t.Fatalf("start() error = %v", err)
	}

	return exp
}
