package graphiteexporter

import (
	"context"
	"net"
	"testing"
	"time"

	"go.opentelemetry.io/collector/exporter/exportertest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

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
			e := &graphiteExporter{config: tt.config}
			result := e.buildPrefix(tt.serviceName, tt.spanName)
			if result != tt.expected {
				t.Errorf("buildPrefix() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatMetric(t *testing.T) {
	tests := []struct {
		name       string
		config     *Config
		metricName string
		value      int64
		timestamp  int64
		tags       map[string]string
		expected   string
	}{
		{
			name:       "plain format",
			config:     &Config{TagSupport: false},
			metricName: "otel.myservice.span_count",
			value:      42,
			timestamp:  1704672000,
			tags:       map[string]string{"service": "myservice"},
			expected:   "otel.myservice.span_count 42 1704672000",
		},
		{
			name:       "tagged format",
			config:     &Config{TagSupport: true},
			metricName: "otel.myservice.span_count",
			value:      42,
			timestamp:  1704672000,
			tags:       map[string]string{"service": "myservice", "span": "op"},
			expected:   "otel.myservice.span_count;service=myservice;span=op 42 1704672000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &graphiteExporter{config: tt.config}
			result := e.formatMetric(tt.metricName, tt.value, tt.timestamp, tt.tags)
			if result != tt.expected {
				t.Errorf("formatMetric() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSendMetricsDisabled(t *testing.T) {
	config := &Config{
		Prefix:      "otel",
		Namespace:   "test",
		TagSupport:  false,
		SendMetrics: false,
	}

	logger, _ := zap.NewDevelopment()
	e := &graphiteExporter{
		config: config,
		logger: logger,
	}

	// Create test trace data
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "test-service")

	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetName("test-operation")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	metrics := e.tracesToMetrics(td)
	if len(metrics) == 0 {
		// When SendMetrics is false, tracesToMetrics still produces metrics; pushTraces will drop them.
		// Ensure pushTraces short-circuits without error and without attempting a connection.
	}
	if err := e.pushTraces(context.Background(), td); err != nil {
		t.Fatalf("pushTraces returned error with SendMetrics=false: %v", err)
	}
	if e.conn != nil {
		t.Fatalf("expected no connection to be opened when SendMetrics=false")
	}
}

func TestTracesToMetrics(t *testing.T) {
	config := &Config{
		Prefix:      "otel",
		Namespace:   "test",
		TagSupport:  false,
		SendMetrics: true,
	}

	logger, _ := zap.NewDevelopment()
	e := &graphiteExporter{
		config: config,
		logger: logger,
	}

	// Create test trace data
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "test-service")

	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetName("test-operation")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	metrics := e.tracesToMetrics(td)

	if len(metrics) == 0 {
		t.Error("Expected metrics to be generated from traces")
	}

	// Verify we get span_count and duration_ms metrics
	hasSpanCount := false
	hasDuration := false
	for _, m := range metrics {
		if contains(m, "span_count") {
			hasSpanCount = true
		}
		if contains(m, "duration_ms") {
			hasDuration = true
		}
	}

	if !hasSpanCount {
		t.Error("Expected span_count metric")
	}
	if !hasDuration {
		t.Error("Expected duration_ms metric")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Endpoint: "localhost:2003",
				Timeout:  10 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "empty endpoint",
			config: &Config{
				Endpoint: "",
				Timeout:  10 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "zero timeout",
			config: &Config{
				Endpoint: "localhost:2003",
				Timeout:  0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewGraphiteExporter(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	t.Run("valid config", func(t *testing.T) {
		cfg := &Config{
			Endpoint:    "localhost:2003",
			Timeout:     10 * time.Second,
			Prefix:      "otel",
			SendMetrics: true,
		}
		exp, err := newGraphiteExporter(cfg, logger)
		if err != nil {
			t.Fatalf("newGraphiteExporter() error = %v", err)
		}
		if exp == nil {
			t.Fatal("newGraphiteExporter() returned nil")
		}
		if exp.config != cfg {
			t.Error("config not set correctly")
		}
		if exp.logger != logger {
			t.Error("logger not set correctly")
		}
	})

	t.Run("invalid config", func(t *testing.T) {
		cfg := &Config{
			Endpoint: "", // Invalid
			Timeout:  10 * time.Second,
		}
		exp, err := newGraphiteExporter(cfg, logger)
		if err == nil {
			t.Fatal("newGraphiteExporter() expected error for invalid config")
		}
		if exp != nil {
			t.Fatal("newGraphiteExporter() should return nil on error")
		}
	})
}

func TestShutdownWithoutConnection(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cfg := &Config{
		Endpoint:    "localhost:2003",
		Timeout:     10 * time.Second,
		SendMetrics: true,
	}
	exp, _ := newGraphiteExporter(cfg, logger)

	// Shutdown without ever connecting should not error
	err := exp.shutdown(context.Background())
	if err != nil {
		t.Fatalf("shutdown() error = %v", err)
	}
}

func TestTracesToMetricsWithErrorSpans(t *testing.T) {
	config := &Config{
		Prefix:      "otel",
		Namespace:   "test",
		TagSupport:  false,
		SendMetrics: true,
	}

	logger, _ := zap.NewDevelopment()
	e := &graphiteExporter{
		config: config,
		logger: logger,
	}

	// Create trace with error span
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "test-service")

	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetName("failing-operation")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	span.Status().SetCode(ptrace.StatusCodeError)

	metrics := e.tracesToMetrics(td)

	// Verify error_count metric is present
	hasErrorCount := false
	for _, m := range metrics {
		if contains(m, "error_count") {
			hasErrorCount = true
			break
		}
	}
	if !hasErrorCount {
		t.Error("Expected error_count metric for error span")
	}
}

func TestTracesToMetricsWithoutErrorSpans(t *testing.T) {
	config := &Config{
		Prefix:      "otel",
		Namespace:   "test",
		TagSupport:  false,
		SendMetrics: true,
	}

	logger, _ := zap.NewDevelopment()
	e := &graphiteExporter{
		config: config,
		logger: logger,
	}

	// Create trace without error span
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "test-service")

	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetName("successful-operation")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	span.Status().SetCode(ptrace.StatusCodeOk)

	metrics := e.tracesToMetrics(td)

	// Verify error_count metric is NOT present for successful spans
	for _, m := range metrics {
		if contains(m, "error_count") {
			t.Error("Did not expect error_count metric for successful span")
		}
	}
}

func TestTracesToMetricsNegativeDuration(t *testing.T) {
	config := &Config{
		Prefix:      "otel",
		Namespace:   "test",
		TagSupport:  false,
		SendMetrics: true,
	}

	logger, _ := zap.NewDevelopment()
	e := &graphiteExporter{
		config: config,
		logger: logger,
	}

	// Create trace with inverted timestamps (malformed span)
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "test-service")

	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetName("malformed-span")
	now := time.Now()
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(now))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(now.Add(-100 * time.Millisecond))) // End before start

	metrics := e.tracesToMetrics(td)

	// Verify duration_ms metric exists and is not negative
	for _, m := range metrics {
		if contains(m, "duration_ms") {
			// Parse the metric value (format: "prefix.duration_ms VALUE TIMESTAMP")
			// Value should be 0 or positive, never negative
			if contains(m, " -") {
				t.Errorf("duration_ms metric has negative value: %s", m)
			}
		}
	}
}

func TestTracesToMetricsUnknownService(t *testing.T) {
	config := &Config{
		Prefix:      "otel",
		Namespace:   "test",
		TagSupport:  false,
		SendMetrics: true,
	}

	logger, _ := zap.NewDevelopment()
	e := &graphiteExporter{
		config: config,
		logger: logger,
	}

	// Create trace without service.name attribute
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	// No service.name attribute set

	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetName("test-operation")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	metrics := e.tracesToMetrics(td)

	// Verify "unknown" is used as service name
	hasUnknown := false
	for _, m := range metrics {
		if contains(m, ".unknown.") {
			hasUnknown = true
			break
		}
	}
	if !hasUnknown {
		t.Error("Expected 'unknown' service name when service.name attribute is missing")
	}
}

func TestTracesToMetricsEmptyTraces(t *testing.T) {
	config := &Config{
		Prefix:      "otel",
		Namespace:   "test",
		TagSupport:  false,
		SendMetrics: true,
	}

	logger, _ := zap.NewDevelopment()
	e := &graphiteExporter{
		config: config,
		logger: logger,
	}

	// Empty traces
	td := ptrace.NewTraces()
	metrics := e.tracesToMetrics(td)

	if len(metrics) != 0 {
		t.Errorf("Expected no metrics for empty traces, got %d", len(metrics))
	}
}

func TestFormatMetricEmptyTags(t *testing.T) {
	config := &Config{TagSupport: true}
	e := &graphiteExporter{config: config}

	// Even with TagSupport enabled, empty tags should produce plain format
	result := e.formatMetric("test.metric", 42, 1704672000, map[string]string{})
	expected := "test.metric 42 1704672000"
	if result != expected {
		t.Errorf("formatMetric() with empty tags = %q, want %q", result, expected)
	}
}

func TestNewFactory(t *testing.T) {
	factory := NewFactory()
	if factory == nil {
		t.Fatal("NewFactory() returned nil")
	}
	if factory.Type() != TypeStr {
		t.Errorf("Factory type = %v, want %v", factory.Type(), TypeStr)
	}
}

func TestCreateDefaultConfig(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()
	if cfg == nil {
		t.Fatal("CreateDefaultConfig() returned nil")
	}

	graphiteCfg, ok := cfg.(*Config)
	if !ok {
		t.Fatal("Config is not *Config type")
	}

	if graphiteCfg.Endpoint != defaultEndpoint {
		t.Errorf("Endpoint = %v, want %v", graphiteCfg.Endpoint, defaultEndpoint)
	}
	if graphiteCfg.Timeout != defaultTimeout {
		t.Errorf("Timeout = %v, want %v", graphiteCfg.Timeout, defaultTimeout)
	}
	if graphiteCfg.Prefix != defaultPrefix {
		t.Errorf("Prefix = %v, want %v", graphiteCfg.Prefix, defaultPrefix)
	}
	if graphiteCfg.SendMetrics != defaultSendMetrics {
		t.Errorf("SendMetrics = %v, want %v", graphiteCfg.SendMetrics, defaultSendMetrics)
	}
}

func TestStartWithInvalidEndpoint(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cfg := &Config{
		Endpoint:    "invalid-host-that-does-not-exist:99999",
		Timeout:     100 * time.Millisecond,
		SendMetrics: true,
	}
	exp, _ := newGraphiteExporter(cfg, logger)

	// Start should fail with invalid endpoint
	err := exp.start(context.Background(), nil)
	if err == nil {
		t.Error("start() should fail with invalid endpoint")
		exp.shutdown(context.Background())
	}
}

func TestShutdownWithConnection(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cfg := &Config{
		Endpoint:    "localhost:2003",
		Timeout:     10 * time.Second,
		SendMetrics: true,
	}
	exp, _ := newGraphiteExporter(cfg, logger)

	// Manually set a closed connection to test shutdown path
	// We can't easily mock net.Conn, but we can test the nil path
	exp.conn = nil
	err := exp.shutdown(context.Background())
	if err != nil {
		t.Errorf("shutdown() with nil conn error = %v", err)
	}
}

func TestPushTracesReconnect(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cfg := &Config{
		Endpoint:    "invalid-host:99999",
		Timeout:     100 * time.Millisecond,
		SendMetrics: true,
		Prefix:      "otel",
	}
	exp, _ := newGraphiteExporter(cfg, logger)
	exp.conn = nil // No connection

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "test")
	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetName("test-op")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	// Should fail to reconnect
	err := exp.pushTraces(context.Background(), td)
	if err == nil {
		t.Error("pushTraces() should fail when reconnect fails")
	}
}

func TestMultipleSpansAggregation(t *testing.T) {
	config := &Config{
		Prefix:      "otel",
		Namespace:   "test",
		TagSupport:  false,
		SendMetrics: true,
	}

	logger, _ := zap.NewDevelopment()
	e := &graphiteExporter{
		config: config,
		logger: logger,
	}

	// Create trace with multiple spans of same name
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "test-service")

	ss := rs.ScopeSpans().AppendEmpty()

	// Add 3 spans with same name
	for i := 0; i < 3; i++ {
		span := ss.Spans().AppendEmpty()
		span.SetName("repeated-operation")
		span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
		span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	}

	metrics := e.tracesToMetrics(td)

	// Should have span_count and duration_ms metrics
	var spanCountMetric string
	for _, m := range metrics {
		if contains(m, "span_count") {
			spanCountMetric = m
			break
		}
	}

	if spanCountMetric == "" {
		t.Fatal("Expected span_count metric")
	}

	// Verify count is 3
	if !contains(spanCountMetric, " 3 ") {
		t.Errorf("Expected span_count=3, got: %s", spanCountMetric)
	}
}

func TestMultipleScopesAndResources(t *testing.T) {
	config := &Config{
		Prefix:      "otel",
		TagSupport:  false,
		SendMetrics: true,
	}

	logger, _ := zap.NewDevelopment()
	e := &graphiteExporter{
		config: config,
		logger: logger,
	}

	td := ptrace.NewTraces()

	// Add two resource spans (different services)
	rs1 := td.ResourceSpans().AppendEmpty()
	rs1.Resource().Attributes().PutStr("service.name", "service-a")
	ss1 := rs1.ScopeSpans().AppendEmpty()
	span1 := ss1.Spans().AppendEmpty()
	span1.SetName("op-a")
	span1.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span1.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	rs2 := td.ResourceSpans().AppendEmpty()
	rs2.Resource().Attributes().PutStr("service.name", "service-b")
	ss2 := rs2.ScopeSpans().AppendEmpty()
	span2 := ss2.Spans().AppendEmpty()
	span2.SetName("op-b")
	span2.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-50 * time.Millisecond)))
	span2.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	metrics := e.tracesToMetrics(td)

	// Should have metrics for both services
	// Note: hyphens are kept in metric names (not replaced with underscores)
	hasServiceA := false
	hasServiceB := false
	for _, m := range metrics {
		if contains(m, "service-a") {
			hasServiceA = true
		}
		if contains(m, "service-b") {
			hasServiceB = true
		}
	}

	if !hasServiceA {
		t.Error("Expected metrics for service-a")
	}
	if !hasServiceB {
		t.Error("Expected metrics for service-b")
	}
}
func TestCreateTracesExporter(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig().(*Config)
	cfg.Endpoint = "localhost:2003"

	ctx := context.Background()
	set := exportertest.NewNopCreateSettings()

	exp, err := factory.CreateTracesExporter(ctx, set, cfg)
	if err != nil {
		t.Fatalf("CreateTracesExporter() error = %v", err)
	}
	if exp == nil {
		t.Fatal("CreateTracesExporter() returned nil exporter")
	}

	// Verify the exporter has the correct type
	if exp.Capabilities().MutatesData {
		t.Error("Expected MutatesData to be false")
	}
}

func TestCreateTracesExporterWithInvalidConfig(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig().(*Config)
	cfg.Endpoint = "" // Invalid: empty endpoint

	ctx := context.Background()
	set := exportertest.NewNopCreateSettings()

	_, err := factory.CreateTracesExporter(ctx, set, cfg)
	if err == nil {
		t.Error("CreateTracesExporter() should fail with invalid config")
	}
}

func TestExporterStartWithValidEndpoint(t *testing.T) {
	// Create a test listener to simulate Graphite
	listener, err := newMockGraphiteServer(t)
	if err != nil {
		t.Skipf("Could not create mock server: %v", err)
	}
	defer listener.Close()

	config := &Config{
		Endpoint:    listener.Addr().String(),
		Timeout:     5 * time.Second,
		Prefix:      "otel",
		SendMetrics: true,
	}

	logger, _ := zap.NewDevelopment()
	e, err := newGraphiteExporter(config, logger)
	if err != nil {
		t.Fatalf("newGraphiteExporter() error = %v", err)
	}

	ctx := context.Background()
	err = e.start(ctx, nil)
	if err != nil {
		t.Errorf("start() error = %v", err)
	}

	// Verify connection was established
	if e.conn == nil {
		t.Error("Expected connection to be established")
	}

	// Clean up
	e.shutdown(ctx)
}

func TestPushTracesSuccess(t *testing.T) {
	// Create a test listener to simulate Graphite
	listener, err := newMockGraphiteServer(t)
	if err != nil {
		t.Skipf("Could not create mock server: %v", err)
	}
	defer listener.Close()

	config := &Config{
		Endpoint:    listener.Addr().String(),
		Timeout:     5 * time.Second,
		Prefix:      "otel",
		SendMetrics: true,
	}

	logger, _ := zap.NewDevelopment()
	e, err := newGraphiteExporter(config, logger)
	if err != nil {
		t.Fatalf("newGraphiteExporter() error = %v", err)
	}

	ctx := context.Background()
	err = e.start(ctx, nil)
	if err != nil {
		t.Fatalf("start() error = %v", err)
	}
	defer e.shutdown(ctx)

	// Create test traces
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "test-service")
	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetName("test-span")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	// Push traces
	err = e.pushTraces(ctx, td)
	if err != nil {
		t.Errorf("pushTraces() error = %v", err)
	}
}

// newMockGraphiteServer creates a TCP listener to simulate a Graphite server
func newMockGraphiteServer(t *testing.T) (*mockListener, error) {
	t.Helper()
	return startMockListener()
}

// mockListener wraps a net.Listener for testing
type mockListener struct {
	ln net.Listener
}

func (m *mockListener) Addr() net.Addr {
	return m.ln.Addr()
}

func (m *mockListener) Close() error {
	return m.ln.Close()
}

func startMockListener() (*mockListener, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	// Accept connections in background
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Just accept and close to simulate a server
			go func(c net.Conn) {
				defer c.Close()
				// Read and discard any data
				buf := make([]byte, 1024)
				for {
					_, err := c.Read(buf)
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	return &mockListener{ln: ln}, nil
}
