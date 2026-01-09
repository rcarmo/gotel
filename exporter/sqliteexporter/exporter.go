package sqliteexporter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

type spanAggregation struct {
	rawSpanName   string
	count         int64
	totalDuration int64
	errorCount    int64
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
		serviceNameRaw := "unknown"
		if serviceAttr, ok := resource.Attributes().Get("service.name"); ok {
			serviceNameRaw = serviceAttr.Str()
		}
		serviceNameMetric := sanitizeMetricName(serviceNameRaw)

		scopeSpans := rs.ScopeSpans()
		for j := 0; j < scopeSpans.Len(); j++ {
			ss := scopeSpans.At(j)
			spans := ss.Spans()

			// Aggregate metrics per span name
			spanAggs := make(map[string]*spanAggregation)

			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				spanNameRaw := span.Name()
				spanNameMetric := sanitizeMetricName(spanNameRaw)

				// Build span JSON for storage
				if e.config.StoreTraces {
					spanJSON := e.spanToJSON(span, resource, ss.Scope())
					spanJSONs = append(spanJSONs, spanJSON)
				}

				// Aggregate metrics
				if e.config.SendMetrics {
					agg, ok := spanAggs[spanNameMetric]
					if !ok {
						agg = &spanAggregation{rawSpanName: spanNameRaw}
						spanAggs[spanNameMetric] = agg
					}
					agg.count++

					duration := span.EndTimestamp().AsTime().Sub(span.StartTimestamp().AsTime()).Milliseconds()
					if duration < 0 {
						duration = 0
					}
					agg.totalDuration += duration

					if span.Status().Code() == ptrace.StatusCodeError {
						agg.errorCount++
					}
				}
			}

			// Generate metrics
			if e.config.SendMetrics {
				for spanNameMetric, agg := range spanAggs {
					prefix := e.buildPrefix(serviceNameMetric, spanNameMetric)
					tags := map[string]string{"service": serviceNameRaw, "span": agg.rawSpanName}
					tagsJSON, _ := json.Marshal(tags)

					metrics = append(metrics, sqlite.MetricRecord{
						Name:      fmt.Sprintf("%s.span_count", prefix),
						Value:     float64(agg.count),
						Timestamp: timestamp,
						Tags:      string(tagsJSON),
					})

					if agg.count > 0 {
						avgDuration := agg.totalDuration / agg.count
						metrics = append(metrics, sqlite.MetricRecord{
							Name:      fmt.Sprintf("%s.duration_ms", prefix),
							Value:     float64(avgDuration),
							Timestamp: timestamp,
							Tags:      string(tagsJSON),
						})
					}

					if agg.errorCount > 0 {
						metrics = append(metrics, sqlite.MetricRecord{
							Name:      fmt.Sprintf("%s.error_count", prefix),
							Value:     float64(agg.errorCount),
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
func (e *sqliteExporter) spanToJSON(span ptrace.Span, resource pcommon.Resource, scope pcommon.InstrumentationScope) []byte {
	// Extract service name from resource
	serviceName := "unknown"
	if serviceAttr, ok := resource.Attributes().Get("service.name"); ok {
		serviceName = serviceAttr.Str()
	}

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

	// Add trace state if present
	if traceState := span.TraceState().AsRaw(); traceState != "" {
		data["trace_state"] = traceState
	}

	// Add resource attributes
	resourceAttrs := make(map[string]interface{})
	resource.Attributes().Range(func(k string, v pcommon.Value) bool {
		resourceAttrs[k] = v.AsRaw()
		return true
	})
	if len(resourceAttrs) > 0 {
		data["resource"] = resourceAttrs
	}

	// Add instrumentation scope
	if scope.Name() != "" {
		scopeData := map[string]interface{}{
			"name": scope.Name(),
		}
		if scope.Version() != "" {
			scopeData["version"] = scope.Version()
		}
		data["scope"] = scopeData
	}

	// Add span attributes
	attrs := make(map[string]interface{})
	span.Attributes().Range(func(k string, v pcommon.Value) bool {
		attrs[k] = v.AsRaw()
		return true
	})
	if len(attrs) > 0 {
		data["attributes"] = attrs
	}

	// Add span links
	if span.Links().Len() > 0 {
		var links []map[string]interface{}
		for i := 0; i < span.Links().Len(); i++ {
			link := span.Links().At(i)
			linkData := map[string]interface{}{
				"trace_id": link.TraceID().String(),
				"span_id":  link.SpanID().String(),
			}
			if link.TraceState().AsRaw() != "" {
				linkData["trace_state"] = link.TraceState().AsRaw()
			}
			if link.Attributes().Len() > 0 {
				linkAttrs := make(map[string]interface{})
				link.Attributes().Range(func(k string, v pcommon.Value) bool {
					linkAttrs[k] = v.AsRaw()
					return true
				})
				linkData["attributes"] = linkAttrs
			}
			links = append(links, linkData)
		}
		data["links"] = links
	}

	// Add events
	if span.Events().Len() > 0 {
		var events []map[string]interface{}
		for i := 0; i < span.Events().Len(); i++ {
			ev := span.Events().At(i)
			eventData := map[string]interface{}{
				"name":      ev.Name(),
				"timestamp": ev.Timestamp().AsTime().UnixNano(),
			}
			if ev.Attributes().Len() > 0 {
				attrs := make(map[string]interface{})
				ev.Attributes().Range(func(k string, v pcommon.Value) bool {
					attrs[k] = v.AsRaw()
					return true
				})
				eventData["attributes"] = attrs
			}
			events = append(events, eventData)
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

	pattern := graphiteToLikePattern(query)

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

func graphiteToLikePattern(query string) string {
	var builder strings.Builder
	builder.Grow(len(query))
	for _, r := range query {
		switch r {
		case '%', '_':
			builder.WriteRune('\\')
			builder.WriteRune(r)
		case '*':
			builder.WriteRune('%')
		case '?':
			builder.WriteRune('_')
		default:
			builder.WriteRune(r)
		}
	}
	return builder.String()
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
