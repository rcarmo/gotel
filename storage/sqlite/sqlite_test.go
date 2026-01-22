package sqlite

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "gotel-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	store, err := New(tmpFile.Name())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer store.Close()

	// Verify WAL mode is enabled
	var journalMode string
	store.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if journalMode != "wal" {
		t.Errorf("Expected WAL mode, got %s", journalMode)
	}
}

func TestInsertAndQuerySpan(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	// Create a test span JSON
	span := map[string]interface{}{
		"trace_id":             "abc123",
		"span_id":              "span1",
		"parent_span_id":       "",
		"service_name":         "test-service",
		"span_name":            "test-operation",
		"start_time_unix_nano": time.Now().UnixNano(),
		"end_time_unix_nano":   time.Now().Add(100 * time.Millisecond).UnixNano(),
		"status":               map[string]interface{}{"code": 0},
	}
	spanJSON, _ := json.Marshal(span)

	// Insert span
	if err := store.InsertSpan(ctx, spanJSON); err != nil {
		t.Fatalf("InsertSpan() error = %v", err)
	}

	// Query by trace ID
	spans, err := store.QueryTraceByID(ctx, "abc123")
	if err != nil {
		t.Fatalf("QueryTraceByID() error = %v", err)
	}
	if len(spans) != 1 {
		t.Errorf("Expected 1 span, got %d", len(spans))
	}
}

func TestInsertSpanBatch(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	// Create multiple test spans
	var spans [][]byte
	for i := 0; i < 100; i++ {
		span := map[string]interface{}{
			"trace_id":             "batch-trace",
			"span_id":              "span" + string(rune(i)),
			"service_name":         "batch-service",
			"span_name":            "batch-operation",
			"start_time_unix_nano": time.Now().UnixNano() + int64(i),
			"end_time_unix_nano":   time.Now().Add(time.Millisecond).UnixNano() + int64(i),
			"status":               map[string]interface{}{"code": 0},
		}
		spanJSON, _ := json.Marshal(span)
		spans = append(spans, spanJSON)
	}

	// Batch insert
	if err := store.InsertSpanBatch(ctx, spans); err != nil {
		t.Fatalf("InsertSpanBatch() error = %v", err)
	}

	// Verify count
	result, _ := store.QueryTraceByID(ctx, "batch-trace")
	if len(result) != 100 {
		t.Errorf("Expected 100 spans, got %d", len(result))
	}
}

func TestQuerySpansWithFilters(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	// Insert spans with different services and statuses
	services := []string{"svc-a", "svc-b", "svc-a"}
	statuses := []int{0, 0, 2} // 2 = error
	for i, svc := range services {
		span := map[string]interface{}{
			"trace_id":             "filter-trace-" + string(rune(i)),
			"span_id":              "span" + string(rune(i)),
			"service_name":         svc,
			"span_name":            "operation",
			"start_time_unix_nano": time.Now().UnixNano(),
			"end_time_unix_nano":   time.Now().Add(time.Millisecond).UnixNano(),
			"status":               map[string]interface{}{"code": statuses[i]},
		}
		spanJSON, _ := json.Marshal(span)
		store.InsertSpan(ctx, spanJSON)
	}

	// Query by service
	spans, err := store.QuerySpans(ctx, SpanQueryOptions{ServiceName: "svc-a"})
	if err != nil {
		t.Fatalf("QuerySpans() error = %v", err)
	}
	if len(spans) != 2 {
		t.Errorf("Expected 2 spans for svc-a, got %d", len(spans))
	}

	// Query by status code (errors)
	errorCode := 2
	spans, err = store.QuerySpans(ctx, SpanQueryOptions{StatusCode: &errorCode})
	if err != nil {
		t.Fatalf("QuerySpans() error = %v", err)
	}
	if len(spans) != 1 {
		t.Errorf("Expected 1 error span, got %d", len(spans))
	}
}

func TestInsertAndQueryMetrics(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	now := time.Now().Unix()
	tags := map[string]string{"service": "test-svc", "span": "test-op"}

	// Insert metrics
	if err := store.InsertMetric(ctx, "span_count", 42, now, tags); err != nil {
		t.Fatalf("InsertMetric() error = %v", err)
	}
	if err := store.InsertMetric(ctx, "duration_ms", 125.5, now, tags); err != nil {
		t.Fatalf("InsertMetric() error = %v", err)
	}

	// Query all metrics
	metrics, err := store.QueryMetrics(ctx, MetricQueryOptions{})
	if err != nil {
		t.Fatalf("QueryMetrics() error = %v", err)
	}
	if len(metrics) != 2 {
		t.Errorf("Expected 2 metrics, got %d", len(metrics))
	}

	// Query by name
	metrics, err = store.QueryMetrics(ctx, MetricQueryOptions{Name: "span_count"})
	if err != nil {
		t.Fatalf("QueryMetrics() error = %v", err)
	}
	if len(metrics) != 1 {
		t.Errorf("Expected 1 metric, got %d", len(metrics))
	}
	if metrics[0].Value != 42 {
		t.Errorf("Expected value 42, got %f", metrics[0].Value)
	}
}

func TestInsertMetricBatch(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	now := time.Now().Unix()
	var metrics []MetricRecord
	for i := 0; i < 100; i++ {
		metrics = append(metrics, MetricRecord{
			Name:      "batch_metric",
			Value:     float64(i),
			Timestamp: now + int64(i),
			Tags:      `{"service":"batch"}`,
		})
	}

	if err := store.InsertMetricBatch(ctx, metrics); err != nil {
		t.Fatalf("InsertMetricBatch() error = %v", err)
	}

	// Verify count
	result, _ := store.QueryMetrics(ctx, MetricQueryOptions{Name: "batch_metric"})
	if len(result) != 100 {
		t.Errorf("Expected 100 metrics, got %d", len(result))
	}
}

func TestListServicesAndOperations(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	// Insert spans with different services and operations
	testData := []struct {
		service string
		op      string
	}{
		{"svc-a", "op1"},
		{"svc-a", "op2"},
		{"svc-b", "op1"},
	}
	for i, td := range testData {
		span := map[string]interface{}{
			"trace_id":             "trace-" + string(rune(i)),
			"span_id":              "span" + string(rune(i)),
			"service_name":         td.service,
			"span_name":            td.op,
			"start_time_unix_nano": time.Now().UnixNano(),
			"end_time_unix_nano":   time.Now().Add(time.Millisecond).UnixNano(),
			"status":               map[string]interface{}{"code": 0},
		}
		spanJSON, _ := json.Marshal(span)
		store.InsertSpan(ctx, spanJSON)
	}

	// List services
	services, err := store.ListServices(ctx)
	if err != nil {
		t.Fatalf("ListServices() error = %v", err)
	}
	if len(services) != 2 {
		t.Errorf("Expected 2 services, got %d", len(services))
	}

	// List operations for svc-a
	ops, err := store.ListOperations(ctx, "svc-a")
	if err != nil {
		t.Fatalf("ListOperations() error = %v", err)
	}
	if len(ops) != 2 {
		t.Errorf("Expected 2 operations for svc-a, got %d", len(ops))
	}
}

func TestStats(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	// Insert some data
	span := map[string]interface{}{
		"trace_id":             "stats-trace",
		"span_id":              "span1",
		"service_name":         "stats-service",
		"span_name":            "stats-op",
		"start_time_unix_nano": time.Now().UnixNano(),
		"end_time_unix_nano":   time.Now().Add(time.Millisecond).UnixNano(),
		"status":               map[string]interface{}{"code": 0},
	}
	spanJSON, _ := json.Marshal(span)
	store.InsertSpan(ctx, spanJSON)
	store.InsertMetric(ctx, "test_metric", 1, time.Now().Unix(), nil)

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.SpanCount != 1 {
		t.Errorf("Expected 1 span, got %d", stats.SpanCount)
	}
	if stats.MetricCount != 1 {
		t.Errorf("Expected 1 metric, got %d", stats.MetricCount)
	}
	if stats.TraceCount != 1 {
		t.Errorf("Expected 1 trace, got %d", stats.TraceCount)
	}
	if stats.ServiceCount != 1 {
		t.Errorf("Expected 1 service, got %d", stats.ServiceCount)
	}
}

func TestCleanup(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	// Insert a span (will have current created_at)
	span := map[string]interface{}{
		"trace_id":             "cleanup-trace",
		"span_id":              "span1",
		"service_name":         "cleanup-service",
		"span_name":            "cleanup-op",
		"start_time_unix_nano": time.Now().UnixNano(),
		"end_time_unix_nano":   time.Now().Add(time.Millisecond).UnixNano(),
		"status":               map[string]interface{}{"code": 0},
	}
	spanJSON, _ := json.Marshal(span)
	store.InsertSpan(ctx, spanJSON)

	// Cleanup with -1 second retention (cutoff = now + 1 second, deletes everything)
	deleted, err := store.Cleanup(ctx, -time.Second)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if deleted != 1 {
		t.Errorf("Expected 1 deleted, got %d", deleted)
	}

	// Verify empty
	stats, _ := store.Stats(ctx)
	if stats.SpanCount != 0 {
		t.Errorf("Expected 0 spans after cleanup, got %d", stats.SpanCount)
	}
}

func TestCheckpoint(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	if err := store.Checkpoint(ctx); err != nil {
		t.Fatalf("Checkpoint() error = %v", err)
	}
}

func TestQuerySpansWithTimeRange(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	baseTime := time.Now().UnixNano()

	// Insert spans at different times
	for i := 0; i < 5; i++ {
		span := map[string]interface{}{
			"trace_id":             "time-trace-" + string(rune('a'+i)),
			"span_id":              "span" + string(rune(i)),
			"service_name":         "time-service",
			"span_name":            "time-op",
			"start_time_unix_nano": baseTime + int64(i*1000000000), // 1 second apart
			"end_time_unix_nano":   baseTime + int64(i*1000000000) + 1000000,
			"status":               map[string]interface{}{"code": 0},
		}
		spanJSON, _ := json.Marshal(span)
		store.InsertSpan(ctx, spanJSON)
	}

	// Query with min time filter
	spans, err := store.QuerySpans(ctx, SpanQueryOptions{
		MinStartTime: baseTime + 2000000000, // After second span
	})
	if err != nil {
		t.Fatalf("QuerySpans() error = %v", err)
	}
	if len(spans) != 3 {
		t.Errorf("Expected 3 spans with min time filter, got %d", len(spans))
	}

	// Query with max time filter
	spans, err = store.QuerySpans(ctx, SpanQueryOptions{
		MaxStartTime: baseTime + 2000000000,
	})
	if err != nil {
		t.Fatalf("QuerySpans() error = %v", err)
	}
	if len(spans) != 3 {
		t.Errorf("Expected 3 spans with max time filter, got %d", len(spans))
	}

	// Query with limit
	spans, err = store.QuerySpans(ctx, SpanQueryOptions{Limit: 2})
	if err != nil {
		t.Fatalf("QuerySpans() error = %v", err)
	}
	if len(spans) != 2 {
		t.Errorf("Expected 2 spans with limit, got %d", len(spans))
	}
}

func TestQueryMetricsWithTimeRange(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	baseTime := time.Now().Unix()

	// Insert metrics at different times
	for i := 0; i < 5; i++ {
		store.InsertMetric(ctx, "time_metric", float64(i*10), baseTime+int64(i*60), nil)
	}

	// Query with time range (timestamps: base, base+60, base+120, base+180, base+240)
	// MinTime 121 includes base+180 and base+240, MaxTime 239 excludes base+240
	metrics, err := store.QueryMetrics(ctx, MetricQueryOptions{
		Name:    "time_metric",
		MinTime: baseTime + 121, // Just after base+120
		MaxTime: baseTime + 239, // Just before base+240
	})
	if err != nil {
		t.Fatalf("QueryMetrics() error = %v", err)
	}
	if len(metrics) != 1 {
		t.Errorf("Expected 1 metric in time range (base+180), got %d", len(metrics))
	}

	// Query with pattern matching
	store.InsertMetric(ctx, "pattern.foo.bar", 1, baseTime, nil)
	store.InsertMetric(ctx, "pattern.foo.baz", 2, baseTime, nil)
	store.InsertMetric(ctx, "pattern.qux.bar", 3, baseTime, nil)

	metrics, err = store.QueryMetrics(ctx, MetricQueryOptions{
		Name:        "pattern.foo.%",
		NamePattern: true,
	})
	if err != nil {
		t.Fatalf("QueryMetrics() error = %v", err)
	}
	if len(metrics) != 2 {
		t.Errorf("Expected 2 metrics matching pattern, got %d", len(metrics))
	}
}

func TestQuerySpansBySpanName(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	// Insert spans with different operation names
	ops := []string{"GET /users", "POST /orders", "GET /users"}
	for i, op := range ops {
		span := map[string]interface{}{
			"trace_id":             "op-trace-" + string(rune('a'+i)),
			"span_id":              "span" + string(rune(i)),
			"service_name":         "op-service",
			"span_name":            op,
			"start_time_unix_nano": time.Now().UnixNano(),
			"end_time_unix_nano":   time.Now().Add(time.Millisecond).UnixNano(),
			"status":               map[string]interface{}{"code": 0},
		}
		spanJSON, _ := json.Marshal(span)
		store.InsertSpan(ctx, spanJSON)
	}

	spans, err := store.QuerySpans(ctx, SpanQueryOptions{SpanName: "GET /users"})
	if err != nil {
		t.Fatalf("QuerySpans() error = %v", err)
	}
	if len(spans) != 2 {
		t.Errorf("Expected 2 spans for GET /users, got %d", len(spans))
	}
}

func TestEmptyQueries(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	// Query empty store
	spans, err := store.QueryTraceByID(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("QueryTraceByID() error = %v", err)
	}
	if len(spans) != 0 {
		t.Errorf("Expected 0 spans, got %d", len(spans))
	}

	services, err := store.ListServices(ctx)
	if err != nil {
		t.Fatalf("ListServices() error = %v", err)
	}
	if len(services) != 0 {
		t.Errorf("Expected 0 services, got %d", len(services))
	}

	ops, err := store.ListOperations(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("ListOperations() error = %v", err)
	}
	if len(ops) != 0 {
		t.Errorf("Expected 0 operations, got %d", len(ops))
	}
}

func TestCleanupWithRetention(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	// Insert a span and metric
	span := map[string]interface{}{
		"trace_id":             "retention-trace",
		"span_id":              "span1",
		"service_name":         "retention-service",
		"span_name":            "retention-op",
		"start_time_unix_nano": time.Now().UnixNano(),
		"end_time_unix_nano":   time.Now().Add(time.Millisecond).UnixNano(),
		"status":               map[string]interface{}{"code": 0},
	}
	spanJSON, _ := json.Marshal(span)
	store.InsertSpan(ctx, spanJSON)
	store.InsertMetric(ctx, "retention_metric", 1, time.Now().Unix(), nil)

	// Cleanup with 1 hour retention should keep everything
	deleted, err := store.Cleanup(ctx, time.Hour)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if deleted != 0 {
		t.Errorf("Expected 0 deleted with hour retention, got %d", deleted)
	}

	stats, _ := store.Stats(ctx)
	if stats.SpanCount != 1 {
		t.Errorf("Expected 1 span after retention cleanup, got %d", stats.SpanCount)
	}
}

func TestConcurrentInserts(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	// Note: SQLite with single writer handles this via mutex
	for i := 0; i < 10; i++ {
		span := map[string]interface{}{
			"trace_id":             "concurrent-trace",
			"span_id":              "span" + string(rune(i)),
			"service_name":         "concurrent-service",
			"span_name":            "concurrent-op",
			"start_time_unix_nano": time.Now().UnixNano() + int64(i),
			"end_time_unix_nano":   time.Now().Add(time.Millisecond).UnixNano() + int64(i),
			"status":               map[string]interface{}{"code": 0},
		}
		spanJSON, _ := json.Marshal(span)
		store.InsertSpan(ctx, spanJSON)
	}

	spans, err := store.QueryTraceByID(ctx, "concurrent-trace")
	if err != nil {
		t.Fatalf("QueryTraceByID() error = %v", err)
	}
	if len(spans) != 10 {
		t.Errorf("Expected 10 spans, got %d", len(spans))
	}
}

func TestQuerySpansByTime(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	baseTime := time.Now()

	// Insert spans at different times
	for i := 0; i < 5; i++ {
		startTime := baseTime.Add(time.Duration(i) * time.Minute)
		endTime := startTime.Add(time.Duration((i+1)*100) * time.Millisecond)
		span := map[string]interface{}{
			"trace_id":             "time-trace-" + string(rune('a'+i)),
			"span_id":              "span" + string(rune(i)),
			"service_name":         "time-service",
			"span_name":            "time-op",
			"start_time_unix_nano": startTime.UnixNano(),
			"end_time_unix_nano":   endTime.UnixNano(),
			"status":               map[string]interface{}{"code": i % 3}, // mix of status codes
		}
		spanJSON, _ := json.Marshal(span)
		store.InsertSpan(ctx, spanJSON)
	}

	// Test basic query
	t.Run("all spans", func(t *testing.T) {
		spans, err := store.QuerySpansByTime(ctx, SpanTimeQueryOptions{})
		if err != nil {
			t.Fatalf("QuerySpansByTime() error = %v", err)
		}
		if len(spans) != 5 {
			t.Errorf("Expected 5 spans, got %d", len(spans))
		}
	})

	// Test with service filter
	t.Run("service filter", func(t *testing.T) {
		spans, err := store.QuerySpansByTime(ctx, SpanTimeQueryOptions{
			ServiceName: "time-service",
		})
		if err != nil {
			t.Fatalf("QuerySpansByTime() error = %v", err)
		}
		if len(spans) != 5 {
			t.Errorf("Expected 5 spans, got %d", len(spans))
		}
	})

	// Test with span name filter
	t.Run("span name filter", func(t *testing.T) {
		spans, err := store.QuerySpansByTime(ctx, SpanTimeQueryOptions{
			SpanName: "time-op",
		})
		if err != nil {
			t.Fatalf("QuerySpansByTime() error = %v", err)
		}
		if len(spans) != 5 {
			t.Errorf("Expected 5 spans, got %d", len(spans))
		}
	})

	// Test with time range
	t.Run("time range", func(t *testing.T) {
		spans, err := store.QuerySpansByTime(ctx, SpanTimeQueryOptions{
			MinStartTime: baseTime.Add(time.Minute).UnixNano(),
			MaxStartTime: baseTime.Add(3 * time.Minute).UnixNano(),
		})
		if err != nil {
			t.Fatalf("QuerySpansByTime() error = %v", err)
		}
		if len(spans) != 3 {
			t.Errorf("Expected 3 spans in time range, got %d", len(spans))
		}
	})

	// Test with status code filter
	t.Run("status code filter", func(t *testing.T) {
		statusCode := 0
		spans, err := store.QuerySpansByTime(ctx, SpanTimeQueryOptions{
			StatusCode: &statusCode,
		})
		if err != nil {
			t.Fatalf("QuerySpansByTime() error = %v", err)
		}
		if len(spans) != 2 { // indices 0 and 3
			t.Errorf("Expected 2 spans with status 0, got %d", len(spans))
		}
	})

	// Test with duration filter
	t.Run("duration filter", func(t *testing.T) {
		minDuration := int64(200) // 200ms
		spans, err := store.QuerySpansByTime(ctx, SpanTimeQueryOptions{
			MinDuration: &minDuration,
		})
		if err != nil {
			t.Fatalf("QuerySpansByTime() error = %v", err)
		}
		// Spans have durations: 100ms, 200ms, 300ms, 400ms, 500ms
		if len(spans) < 3 {
			t.Errorf("Expected at least 3 spans with min duration 200ms, got %d", len(spans))
		}
	})

	// Test with limit and offset
	t.Run("limit and offset", func(t *testing.T) {
		spans, err := store.QuerySpansByTime(ctx, SpanTimeQueryOptions{
			Limit:  2,
			Offset: 1,
		})
		if err != nil {
			t.Fatalf("QuerySpansByTime() error = %v", err)
		}
		if len(spans) != 2 {
			t.Errorf("Expected 2 spans with limit/offset, got %d", len(spans))
		}
	})
}

func TestSearchTraces(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	baseTime := time.Now()

	// Insert spans with different services and operations
	services := []string{"svc-a", "svc-a", "svc-b", "svc-c", "svc-c"}
	ops := []string{"op1", "op2", "op1", "op3", "op3"}

	for i := 0; i < 5; i++ {
		startTime := baseTime.Add(time.Duration(i) * time.Minute)
		span := map[string]interface{}{
			"trace_id":             "search-trace-" + string(rune('a'+i)),
			"span_id":              "span" + string(rune(i)),
			"parent_span_id":       "", // root span
			"service_name":         services[i],
			"span_name":            ops[i],
			"start_time_unix_nano": startTime.UnixNano(),
			"end_time_unix_nano":   startTime.Add(100 * time.Millisecond).UnixNano(),
			"status":               map[string]interface{}{"code": 0},
		}
		spanJSON, _ := json.Marshal(span)
		store.InsertSpan(ctx, spanJSON)
	}

	// Test basic search
	t.Run("all traces", func(t *testing.T) {
		traces, err := store.SearchTraces(ctx, TraceSearchOptions{})
		if err != nil {
			t.Fatalf("SearchTraces() error = %v", err)
		}
		if len(traces) != 5 {
			t.Errorf("Expected 5 traces, got %d", len(traces))
		}
	})

	// Test search by service
	t.Run("by service", func(t *testing.T) {
		traces, err := store.SearchTraces(ctx, TraceSearchOptions{
			ServiceName: "svc-a",
		})
		if err != nil {
			t.Fatalf("SearchTraces() error = %v", err)
		}
		if len(traces) != 2 {
			t.Errorf("Expected 2 traces for svc-a, got %d", len(traces))
		}
	})

	// Test search by span name
	t.Run("by span name", func(t *testing.T) {
		traces, err := store.SearchTraces(ctx, TraceSearchOptions{
			SpanName: "op3",
		})
		if err != nil {
			t.Fatalf("SearchTraces() error = %v", err)
		}
		if len(traces) != 2 {
			t.Errorf("Expected 2 traces for op3, got %d", len(traces))
		}
	})

	// Test search by time range
	t.Run("by time range", func(t *testing.T) {
		traces, err := store.SearchTraces(ctx, TraceSearchOptions{
			MinStartTime: baseTime.Add(time.Minute).UnixNano(),
			MaxStartTime: baseTime.Add(3 * time.Minute).UnixNano(),
		})
		if err != nil {
			t.Fatalf("SearchTraces() error = %v", err)
		}
		if len(traces) != 3 {
			t.Errorf("Expected 3 traces in time range, got %d", len(traces))
		}
	})

	// Test search with limit
	t.Run("with limit", func(t *testing.T) {
		traces, err := store.SearchTraces(ctx, TraceSearchOptions{
			Limit: 2,
		})
		if err != nil {
			t.Fatalf("SearchTraces() error = %v", err)
		}
		if len(traces) != 2 {
			t.Errorf("Expected 2 traces with limit, got %d", len(traces))
		}
	})

	// Test trace summary fields
	t.Run("summary fields", func(t *testing.T) {
		traces, err := store.SearchTraces(ctx, TraceSearchOptions{
			ServiceName: "svc-a",
			SpanName:    "op1",
			Limit:       1,
		})
		if err != nil {
			t.Fatalf("SearchTraces() error = %v", err)
		}
		if len(traces) == 0 {
			t.Fatal("Expected at least 1 trace")
		}
		trace := traces[0]
		if trace.TraceID == "" {
			t.Error("Expected TraceID to be set")
		}
		if trace.RootServiceName != "svc-a" {
			t.Errorf("Expected RootServiceName svc-a, got %s", trace.RootServiceName)
		}
		if trace.RootTraceName != "op1" {
			t.Errorf("Expected RootTraceName op1, got %s", trace.RootTraceName)
		}
	})
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "gotel-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })
	tmpFile.Close()

	store, err := New(tmpFile.Name())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return store
}
