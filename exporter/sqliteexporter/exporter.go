package sqliteexporter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"

	"github.com/gotel/storage/sqlite"
)

// sqliteExporter exports traces to SQLite and serves query API
type sqliteExporter struct {
	config     *Config
	logger     *zap.Logger
	store      *sqlite.Store
	server     *http.Server
	cleanupCtx context.Context
	cancelFunc context.CancelFunc
	wg         sync.WaitGroup
}

// newSQLiteExporter creates a new SQLite exporter
func newSQLiteExporter(config *Config, logger *zap.Logger) (*sqliteExporter, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &sqliteExporter{
		config: config,
		logger: logger,
	}, nil
}

// start initializes the SQLite store and HTTP server
func (e *sqliteExporter) start(ctx context.Context, host component.Host) error {
	store, err := sqlite.New(e.config.DBPath)
	if err != nil {
		return fmt.Errorf("failed to open SQLite database at %s: %w", e.config.DBPath, err)
	}
	e.store = store

	e.logger.Info("SQLite store opened",
		zap.String("db_path", e.config.DBPath),
		zap.Duration("retention", e.config.Retention))

	// Start cleanup goroutine
	e.cleanupCtx, e.cancelFunc = context.WithCancel(context.Background())
	e.wg.Add(1)
	go e.runCleanup()

	// Start query HTTP server if port configured
	if e.config.QueryPort > 0 {
		e.wg.Add(1)
		go e.startQueryServer()
	}

	return nil
}

// shutdown closes the store and HTTP server
func (e *sqliteExporter) shutdown(ctx context.Context) error {
	if e.cancelFunc != nil {
		e.cancelFunc()
	}

	if e.server != nil {
		e.server.Shutdown(ctx)
	}

	e.wg.Wait()

	if e.store != nil {
		// Checkpoint before closing
		e.store.Checkpoint(ctx)
		return e.store.Close()
	}
	return nil
}

// pushTraces converts traces to SQLite records
func (e *sqliteExporter) pushTraces(ctx context.Context, td ptrace.Traces) error {
	var spanJSONs [][]byte
	var metrics []sqlite.MetricRecord
	timestamp := time.Now().Unix()

	resourceSpans := td.ResourceSpans()
	for i := 0; i < resourceSpans.Len(); i++ {
		rs := resourceSpans.At(i)
		resource := rs.Resource()

		// Extract service name
		serviceName := "unknown"
		if serviceAttr, ok := resource.Attributes().Get("service.name"); ok {
			serviceName = sanitizeMetricName(serviceAttr.Str())
		}

		scopeSpans := rs.ScopeSpans()
		for j := 0; j < scopeSpans.Len(); j++ {
			ss := scopeSpans.At(j)
			spans := ss.Spans()

			// Aggregate metrics per span name
			spanCounts := make(map[string]int64)
			spanDurations := make(map[string]int64)
			spanErrors := make(map[string]int64)

			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				spanName := sanitizeMetricName(span.Name())

				// Build span JSON for storage
				if e.config.StoreTraces {
					spanJSON := e.spanToJSON(span, serviceName)
					spanJSONs = append(spanJSONs, spanJSON)
				}

				// Aggregate metrics
				if e.config.SendMetrics {
					spanCounts[spanName]++

					duration := span.EndTimestamp().AsTime().Sub(span.StartTimestamp().AsTime()).Milliseconds()
					if duration < 0 {
						duration = 0
					}
					spanDurations[spanName] += duration

					if span.Status().Code() == ptrace.StatusCodeError {
						spanErrors[spanName]++
					}
				}
			}

			// Generate metrics
			if e.config.SendMetrics {
				for spanName, count := range spanCounts {
					prefix := e.buildPrefix(serviceName, spanName)
					tags := map[string]string{"service": serviceName, "span": spanName}
					tagsJSON, _ := json.Marshal(tags)

					metrics = append(metrics, sqlite.MetricRecord{
						Name:      fmt.Sprintf("%s.span_count", prefix),
						Value:     float64(count),
						Timestamp: timestamp,
						Tags:      string(tagsJSON),
					})

					if count > 0 {
						avgDuration := spanDurations[spanName] / count
						metrics = append(metrics, sqlite.MetricRecord{
							Name:      fmt.Sprintf("%s.duration_ms", prefix),
							Value:     float64(avgDuration),
							Timestamp: timestamp,
							Tags:      string(tagsJSON),
						})
					}

					if errorCount := spanErrors[spanName]; errorCount > 0 {
						metrics = append(metrics, sqlite.MetricRecord{
							Name:      fmt.Sprintf("%s.error_count", prefix),
							Value:     float64(errorCount),
							Timestamp: timestamp,
							Tags:      string(tagsJSON),
						})
					}
				}
			}
		}
	}

	// Batch insert spans
	if len(spanJSONs) > 0 {
		if err := e.store.InsertSpanBatch(ctx, spanJSONs); err != nil {
			return fmt.Errorf("failed to insert spans: %w", err)
		}
	}

	// Batch insert metrics
	if len(metrics) > 0 {
		if err := e.store.InsertMetricBatch(ctx, metrics); err != nil {
			return fmt.Errorf("failed to insert metrics: %w", err)
		}
	}

	e.logger.Debug("Stored traces",
		zap.Int("spans", len(spanJSONs)),
		zap.Int("metrics", len(metrics)))

	return nil
}

// spanToJSON converts a span to JSON for storage
func (e *sqliteExporter) spanToJSON(span ptrace.Span, serviceName string) []byte {
	data := map[string]interface{}{
		"trace_id":             span.TraceID().String(),
		"span_id":              span.SpanID().String(),
		"parent_span_id":       span.ParentSpanID().String(),
		"service_name":         serviceName,
		"span_name":            span.Name(),
		"kind":                 span.Kind().String(),
		"start_time_unix_nano": span.StartTimestamp().AsTime().UnixNano(),
		"end_time_unix_nano":   span.EndTimestamp().AsTime().UnixNano(),
		"status": map[string]interface{}{
			"code":    int(span.Status().Code()),
			"message": span.Status().Message(),
		},
	}

	// Add attributes
	attrs := make(map[string]interface{})
	span.Attributes().Range(func(k string, v pcommon.Value) bool {
		attrs[k] = v.AsRaw()
		return true
	})
	if len(attrs) > 0 {
		data["attributes"] = attrs
	}

	// Add events
	if span.Events().Len() > 0 {
		var events []map[string]interface{}
		for i := 0; i < span.Events().Len(); i++ {
			ev := span.Events().At(i)
			events = append(events, map[string]interface{}{
				"name":      ev.Name(),
				"timestamp": ev.Timestamp().AsTime().UnixNano(),
			})
		}
		data["events"] = events
	}

	result, _ := json.Marshal(data)
	return result
}

// buildPrefix constructs the metric prefix
func (e *sqliteExporter) buildPrefix(serviceName, spanName string) string {
	parts := []string{e.config.Prefix}
	if e.config.Namespace != "" {
		parts = append(parts, e.config.Namespace)
	}
	parts = append(parts, serviceName, spanName)
	return strings.Join(parts, ".")
}

// runCleanup periodically cleans up old data
func (e *sqliteExporter) runCleanup() {
	defer e.wg.Done()

	ticker := time.NewTicker(e.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.cleanupCtx.Done():
			return
		case <-ticker.C:
			deleted, err := e.store.Cleanup(e.cleanupCtx, e.config.Retention)
			if err != nil {
				e.logger.Error("Cleanup failed", zap.Error(err))
			} else if deleted > 0 {
				e.logger.Info("Cleanup completed", zap.Int64("deleted", deleted))
			}
		}
	}
}

// startQueryServer starts the HTTP query API
func (e *sqliteExporter) startQueryServer() {
	defer e.wg.Done()

	mux := http.NewServeMux()

	// Tempo-compatible endpoints
	mux.HandleFunc("/api/traces/", e.handleGetTrace)
	mux.HandleFunc("/api/search", e.handleSearchTraces)
	mux.HandleFunc("/api/services", e.handleListServices)

	// Graphite-compatible endpoints
	mux.HandleFunc("/render", e.handleRenderMetrics)
	mux.HandleFunc("/metrics/find", e.handleFindMetrics)

	// Status endpoints
	mux.HandleFunc("/api/status", e.handleStatus)
	mux.HandleFunc("/ready", e.handleReady)

	e.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", e.config.QueryPort),
		Handler: mux,
	}

	e.logger.Info("Starting query server", zap.Int("port", e.config.QueryPort))

	if err := e.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		e.logger.Error("Query server error", zap.Error(err))
	}
}

// handleGetTrace returns a single trace by ID
func (e *sqliteExporter) handleGetTrace(w http.ResponseWriter, r *http.Request) {
	traceID := strings.TrimPrefix(r.URL.Path, "/api/traces/")
	if traceID == "" {
		http.Error(w, "trace_id required", http.StatusBadRequest)
		return
	}

	spans, err := e.store.QueryTraceByID(r.Context(), traceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"batches": []interface{}{
			map[string]interface{}{
				"spans": spans,
			},
		},
	})
}

// handleSearchTraces searches for traces
func (e *sqliteExporter) handleSearchTraces(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	opts := sqlite.SpanQueryOptions{
		ServiceName: q.Get("service"),
		SpanName:    q.Get("operation"),
		Limit:       20,
	}

	spans, err := e.store.QuerySpans(r.Context(), opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"traces": spans,
	})
}

// handleListServices lists available services
func (e *sqliteExporter) handleListServices(w http.ResponseWriter, r *http.Request) {
	services, err := e.store.ListServices(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(services)
}

// handleRenderMetrics returns metric data (Graphite-compatible)
func (e *sqliteExporter) handleRenderMetrics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	target := q.Get("target")

	opts := sqlite.MetricQueryOptions{
		Name:        target,
		NamePattern: strings.Contains(target, "%") || strings.Contains(target, "*"),
	}

	// Convert wildcards to SQL LIKE pattern
	if opts.NamePattern {
		opts.Name = strings.ReplaceAll(opts.Name, "*", "%")
	}

	metrics, err := e.store.QueryMetrics(r.Context(), opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to Graphite format
	result := make([]map[string]interface{}, 0)
	grouped := make(map[string][]interface{})

	for _, m := range metrics {
		grouped[m.Name] = append(grouped[m.Name], []interface{}{m.Value, m.Timestamp})
	}

	for name, datapoints := range grouped {
		result = append(result, map[string]interface{}{
			"target":     name,
			"datapoints": datapoints,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleFindMetrics finds metric names (Graphite-compatible)
func (e *sqliteExporter) handleFindMetrics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := q.Get("query")

	pattern := strings.ReplaceAll(query, "*", "%")

	metrics, err := e.store.QueryMetrics(r.Context(), sqlite.MetricQueryOptions{
		Name:        pattern,
		NamePattern: true,
		Limit:       1000,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get unique metric names
	names := make(map[string]bool)
	for _, m := range metrics {
		names[m.Name] = true
	}

	result := make([]map[string]interface{}, 0)
	for name := range names {
		result = append(result, map[string]interface{}{
			"text":          name,
			"id":            name,
			"expandable":    false,
			"allowChildren": false,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleStatus returns storage statistics
func (e *sqliteExporter) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats, err := e.store.Stats(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleReady returns ready status
func (e *sqliteExporter) handleReady(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ready"))
}

// sanitizeMetricName replaces invalid characters in metric names
func sanitizeMetricName(name string) string {
	replacer := strings.NewReplacer(
		" ", "_",
		"/", "_",
		"\\", "_",
		":", "_",
		"=", "_",
		";", "_",
		"(", "_",
		")", "_",
		"[", "_",
		"]", "_",
		"{", "_",
		"}", "_",
	)
	return replacer.Replace(name)
}

// formatMetric formats a metric for output (used for tag support)
func (e *sqliteExporter) formatMetric(name string, value float64, timestamp int64, tags map[string]string) string {
	if e.config.TagSupport && len(tags) > 0 {
		var tagParts []string
		keys := make([]string, 0, len(tags))
		for k := range tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := tags[k]
			tagParts = append(tagParts, fmt.Sprintf("%s=%s", k, sanitizeMetricName(v)))
		}
		return fmt.Sprintf("%s;%s %f %d", name, strings.Join(tagParts, ";"), value, timestamp)
	}
	return fmt.Sprintf("%s %f %d", name, value, timestamp)
}
