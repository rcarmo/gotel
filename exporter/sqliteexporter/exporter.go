package sqliteexporter

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
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

	// Tempo-compatible endpoints (subset used by Grafana)
	mux.HandleFunc("/api/echo", e.handleEcho)
	mux.HandleFunc("/api/traces/", e.handleGetTrace)
	mux.HandleFunc("/api/v2/traces/", e.handleGetTrace)
	mux.HandleFunc("/api/search", e.handleSearchTraces)
	mux.HandleFunc("/api/v2/search", e.handleSearchTraces)
	mux.HandleFunc("/api/search/tags", e.handleSearchTags)
	mux.HandleFunc("/api/v2/search/tags", e.handleSearchTagsV2)
	mux.HandleFunc("/api/search/tag/", e.handleSearchTagValues)
	mux.HandleFunc("/api/v2/search/tag/", e.handleSearchTagValuesV2)

	// Kept for backwards compatibility with earlier experiments
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
	if strings.HasPrefix(r.URL.Path, "/api/v2/traces/") {
		traceID = strings.TrimPrefix(r.URL.Path, "/api/v2/traces/")
	}
	if traceID == "" {
		http.Error(w, "trace_id required", http.StatusBadRequest)
		return
	}

	spans, err := e.store.QueryTraceByID(r.Context(), traceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Tempo returns OTLP JSON by default. We produce a best-effort OTLP-ish JSON
	// shape using the fields we persist.
	resourceSpans := groupSpansAsOTLPResourceSpans(spans)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"resourceSpans": resourceSpans,
	})
}

// handleSearchTraces searches for traces
func (e *sqliteExporter) handleSearchTraces(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := 20
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	serviceName := q.Get("service")
	spanName := q.Get("operation")

	// Tempo tag search uses logfmt encoding.
	if serviceName == "" {
		if tags := q.Get("tags"); tags != "" {
			if s := extractServiceFromTags(tags); s != "" {
				serviceName = s
			}
		}
	}

	// TraceQL search uses the q parameter. We only extract the common
	// resource.service.name / service.name matcher for now.
	if serviceName == "" {
		if traceQL := q.Get("q"); traceQL != "" {
			if s := extractServiceFromTraceQL(traceQL); s != "" {
				serviceName = s
			}
		}
	}

	minStartNs := int64(0)
	maxStartNs := int64(0)
	// Tempo search uses start/end as unix epoch seconds.
	if v := q.Get("start"); v != "" {
		if sec, err := strconv.ParseInt(v, 10, 64); err == nil && sec > 0 {
			minStartNs = sec * int64(time.Second)
		}
	}
	if v := q.Get("end"); v != "" {
		if sec, err := strconv.ParseInt(v, 10, 64); err == nil && sec > 0 {
			maxStartNs = sec * int64(time.Second)
		}
	}

	traces, err := e.store.SearchTraces(r.Context(), sqlite.TraceSearchOptions{
		ServiceName:  serviceName,
		SpanName:     spanName,
		MinStartTime: minStartNs,
		MaxStartTime: maxStartNs,
		Limit:        limit,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	results := make([]map[string]interface{}, 0, len(traces))
	for _, t := range traces {
		results = append(results, map[string]interface{}{
			"traceID":           t.TraceID,
			"rootServiceName":   t.RootServiceName,
			"rootTraceName":     t.RootTraceName,
			"startTimeUnixNano": fmt.Sprintf("%d", t.StartTimeUnixNano),
			"durationMs":        t.DurationMs,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"traces":  results,
		"metrics": map[string]interface{}{},
	})
}

func (e *sqliteExporter) handleEcho(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("echo"))
}

func (e *sqliteExporter) handleSearchTags(w http.ResponseWriter, r *http.Request) {
	// Minimal set of tags; Grafana commonly asks for these.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tagNames": []string{"service.name", "span.name", "status"},
		"metrics":  map[string]interface{}{},
	})
}

func (e *sqliteExporter) handleSearchTagsV2(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"scopes": []interface{}{
			map[string]interface{}{"name": "resource", "tags": []string{"service.name"}},
			map[string]interface{}{"name": "span", "tags": []string{"name"}},
			map[string]interface{}{"name": "intrinsic", "tags": []string{"duration", "status"}},
		},
		"metrics": map[string]interface{}{},
	})
}

func (e *sqliteExporter) handleSearchTagValues(w http.ResponseWriter, r *http.Request) {
	tag := strings.TrimPrefix(r.URL.Path, "/api/search/tag/")
	tag = strings.TrimSuffix(tag, "/values")
	if strings.HasSuffix(tag, "/values") {
		tag = strings.TrimSuffix(tag, "/values")
	}
	tag = strings.TrimPrefix(tag, ".")

	// Only support service.name for now.
	if tag != "service.name" && tag != "resource.service.name" {
		http.Error(w, "unsupported tag", http.StatusNotFound)
		return
	}

	services, err := e.store.ListServices(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tagValues": services,
		"metrics":   map[string]interface{}{},
	})
}

func (e *sqliteExporter) handleSearchTagValuesV2(w http.ResponseWriter, r *http.Request) {
	tag := strings.TrimPrefix(r.URL.Path, "/api/v2/search/tag/")
	tag = strings.TrimSuffix(tag, "/values")
	tag = strings.TrimPrefix(tag, ".")

	if tag != "service.name" && tag != "resource.service.name" {
		http.Error(w, "unsupported tag", http.StatusNotFound)
		return
	}

	services, err := e.store.ListServices(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	values := make([]map[string]interface{}, 0, len(services))
	for _, s := range services {
		values = append(values, map[string]interface{}{"type": "string", "value": s})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tagValues": values,
		"metrics":   map[string]interface{}{},
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
	targets := q["target"]
	if len(targets) == 0 {
		targets = []string{q.Get("target")}
	}
	var allResults []map[string]interface{}

	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}

		// Support a small subset of Graphite functions used in dashboards.
		if inner, idxs, ok := parseAliasByNode(target); ok {
			series, err := e.queryMetricSeries(r.Context(), inner)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			for name, datapoints := range series {
				allResults = append(allResults, map[string]interface{}{
					"target":     aliasByNode(name, idxs),
					"datapoints": datapoints,
				})
			}
			continue
		}

		series, err := e.queryMetricSeries(r.Context(), target)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for name, datapoints := range series {
			allResults = append(allResults, map[string]interface{}{
				"target":     name,
				"datapoints": datapoints,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allResults)
}

// handleFindMetrics finds metric names (Graphite-compatible)
func (e *sqliteExporter) handleFindMetrics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("query"))
	if query == "" {
		http.Error(w, "query required", http.StatusBadRequest)
		return
	}

	// Support aliasByNode(...) in find queries for template variables.
	if inner, idxs, ok := parseAliasByNode(query); ok {
		found, err := e.findMetricNodes(r.Context(), inner)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		result := make([]map[string]interface{}, 0, len(found))
		for _, name := range found {
			alias := aliasByNode(name, idxs)
			result = append(result, map[string]interface{}{
				"text":          alias,
				"id":            alias,
				"expandable":    false,
				"allowChildren": false,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
		return
	}

	found, err := e.findMetricNodes(r.Context(), query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	result := make([]map[string]interface{}, 0, len(found))
	for _, name := range found {
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

func (e *sqliteExporter) queryMetricSeries(ctx context.Context, target string) (map[string][]interface{}, error) {
	pattern := target
	namePattern := strings.Contains(pattern, "*") || strings.Contains(pattern, "?")
	if namePattern {
		pattern = graphiteToLikePattern(pattern)
	}

	metrics, err := e.store.QueryMetrics(ctx, sqlite.MetricQueryOptions{
		Name:        pattern,
		NamePattern: namePattern,
	})
	if err != nil {
		return nil, err
	}

	grouped := make(map[string][]interface{})
	for _, m := range metrics {
		grouped[m.Name] = append(grouped[m.Name], []interface{}{m.Value, m.Timestamp})
	}
	return grouped, nil
}

func (e *sqliteExporter) findMetricNodes(ctx context.Context, query string) ([]string, error) {
	pattern := graphiteToLikePattern(query)
	metrics, err := e.store.QueryMetrics(ctx, sqlite.MetricQueryOptions{
		Name:        pattern,
		NamePattern: true,
		Limit:       2000,
	})
	if err != nil {
		return nil, err
	}

	// Approximate Graphite find semantics: return unique nodes matching the query depth.
	depth := len(strings.Split(query, "."))
	nodes := make(map[string]struct{})
	for _, m := range metrics {
		parts := strings.Split(m.Name, ".")
		if len(parts) < depth {
			continue
		}
		node := strings.Join(parts[:depth], ".")
		nodes[node] = struct{}{}
	}

	out := make([]string, 0, len(nodes))
	for n := range nodes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

func parseAliasByNode(expr string) (string, []int, bool) {
	expr = strings.TrimSpace(expr)
	if !strings.HasPrefix(expr, "aliasByNode(") || !strings.HasSuffix(expr, ")") {
		return "", nil, false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(expr, "aliasByNode("), ")")
	args := splitTopLevelCSV(inner)
	if len(args) < 2 {
		return "", nil, false
	}

	pattern := strings.TrimSpace(args[0])
	pattern = strings.Trim(pattern, "\"'")

	idxs := make([]int, 0, len(args)-1)
	for _, a := range args[1:] {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		idx, err := strconv.Atoi(a)
		if err != nil {
			return "", nil, false
		}
		idxs = append(idxs, idx)
	}
	if len(idxs) == 0 {
		return "", nil, false
	}
	return pattern, idxs, true
}

func splitTopLevelCSV(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

func aliasByNode(metric string, idxs []int) string {
	parts := strings.Split(metric, ".")
	if len(parts) == 0 {
		return metric
	}

	selected := make([]string, 0, len(idxs))
	for _, idx := range idxs {
		p := idx
		if p < 0 {
			p = len(parts) + p
		}
		if p < 0 || p >= len(parts) {
			continue
		}
		selected = append(selected, parts[p])
	}
	if len(selected) == 0 {
		return metric
	}
	return strings.Join(selected, ".")
}

func extractServiceFromTags(tags string) string {
	// logfmt-ish: key=value key2="value with spaces"
	fields := strings.Fields(tags)
	for _, f := range fields {
		kv := strings.SplitN(f, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), "\"")
		if key == "service.name" || key == "resource.service.name" {
			return val
		}
	}
	return ""
}

func extractServiceFromTraceQL(q string) string {
	// Best-effort matcher for the common cases:
	// {resource.service.name="foo"} or {service.name="foo"}
	re := regexp.MustCompile(`(?:resource\.)?service\.name\s*=\s*"([^"]+)"`)
	m := re.FindStringSubmatch(q)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

func groupSpansAsOTLPResourceSpans(spans []json.RawMessage) []interface{} {
	// Group by resource.service.name (fallback to service_name) and scope.name.
	type scopeKey struct {
		service string
		scope   string
	}
	resources := make(map[string]map[string][]map[string]interface{})
	resourceAttrs := make(map[string][]map[string]interface{})
	scopeAttrs := make(map[scopeKey]map[string]interface{})

	for _, raw := range spans {
		var m map[string]interface{}
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}

		service := ""
		if res, ok := m["resource"].(map[string]interface{}); ok {
			if v, ok := res["service.name"].(string); ok {
				service = v
			}
			if service == "" {
				if v, ok := res["service.name"].(string); ok {
					service = v
				}
			}
			if _, exists := resourceAttrs[service]; !exists {
				resourceAttrs[service] = mapToOTLPAttributes(res)
			}
		}
		if service == "" {
			if v, ok := m["service_name"].(string); ok {
				service = v
			}
		}
		if service == "" {
			service = "unknown"
		}

		scopeName := ""
		if scope, ok := m["scope"].(map[string]interface{}); ok {
			if v, ok := scope["name"].(string); ok {
				scopeName = v
			}
			if _, exists := scopeAttrs[scopeKey{service: service, scope: scopeName}]; !exists {
				scopeAttrs[scopeKey{service: service, scope: scopeName}] = map[string]interface{}{
					"name": scopeName,
				}
			}
		}

		if _, ok := resources[service]; !ok {
			resources[service] = make(map[string][]map[string]interface{})
		}

		otlpSpan := toOTLPSpan(m)
		resources[service][scopeName] = append(resources[service][scopeName], otlpSpan)
	}

	var out []interface{}
	for service, scopes := range resources {
		var scopeSpans []interface{}
		for scopeName, spanList := range scopes {
			scopeSpans = append(scopeSpans, map[string]interface{}{
				"scope": scopeAttrs[scopeKey{service: service, scope: scopeName}],
				"spans": spanList,
			})
		}

		out = append(out, map[string]interface{}{
			"resource": map[string]interface{}{
				"attributes": resourceAttrs[service],
			},
			"scopeSpans": scopeSpans,
		})
	}

	return out
}

func toOTLPSpan(m map[string]interface{}) map[string]interface{} {
	traceID, _ := m["trace_id"].(string)
	spanID, _ := m["span_id"].(string)
	parentSpanID, _ := m["parent_span_id"].(string)
	name, _ := m["span_name"].(string)
	kind, _ := m["kind"].(string)

	start := fmt.Sprintf("%v", m["start_time_unix_nano"])
	end := fmt.Sprintf("%v", m["end_time_unix_nano"])

	attrs := []map[string]interface{}{}
	if a, ok := m["attributes"].(map[string]interface{}); ok {
		attrs = mapToOTLPAttributes(a)
	}

	status := map[string]interface{}{}
	if st, ok := m["status"].(map[string]interface{}); ok {
		code := "STATUS_CODE_UNSET"
		if c, ok := st["code"].(float64); ok {
			if int(c) == 2 {
				code = "STATUS_CODE_ERROR"
			} else if int(c) == 0 {
				code = "STATUS_CODE_OK"
			}
		}
		status["code"] = code
		if msg, ok := st["message"].(string); ok && msg != "" {
			status["message"] = msg
		}
	}

	otlpKind := "SPAN_KIND_UNSPECIFIED"
	switch strings.ToLower(kind) {
	case "internal":
		otlpKind = "SPAN_KIND_INTERNAL"
	case "server":
		otlpKind = "SPAN_KIND_SERVER"
	case "client":
		otlpKind = "SPAN_KIND_CLIENT"
	case "producer":
		otlpKind = "SPAN_KIND_PRODUCER"
	case "consumer":
		otlpKind = "SPAN_KIND_CONSUMER"
	}

	out := map[string]interface{}{
		"traceId":           traceID,
		"spanId":            spanID,
		"name":              name,
		"kind":              otlpKind,
		"startTimeUnixNano": start,
		"endTimeUnixNano":   end,
		"attributes":        attrs,
		"status":            status,
	}
	if parentSpanID != "" && parentSpanID != "0000000000000000" {
		out["parentSpanId"] = parentSpanID
	}

	if evs, ok := m["events"].([]interface{}); ok {
		converted := make([]map[string]interface{}, 0, len(evs))
		for _, ev := range evs {
			em, ok := ev.(map[string]interface{})
			if !ok {
				continue
			}
			ce := map[string]interface{}{}
			if n, ok := em["name"].(string); ok {
				ce["name"] = n
			}
			if ts, ok := em["timestamp"].(float64); ok {
				ce["timeUnixNano"] = fmt.Sprintf("%d", int64(ts))
			}
			if at, ok := em["attributes"].(map[string]interface{}); ok {
				ce["attributes"] = mapToOTLPAttributes(at)
			}
			converted = append(converted, ce)
		}
		if len(converted) > 0 {
			out["events"] = converted
		}
	}

	return out
}

func mapToOTLPAttributes(m map[string]interface{}) []map[string]interface{} {
	attrs := make([]map[string]interface{}, 0, len(m))
	for k, v := range m {
		attrs = append(attrs, map[string]interface{}{
			"key":   k,
			"value": toOTLPAnyValue(v),
		})
	}
	sort.Slice(attrs, func(i, j int) bool { return attrs[i]["key"].(string) < attrs[j]["key"].(string) })
	return attrs
}

func toOTLPAnyValue(v interface{}) map[string]interface{} {
	switch t := v.(type) {
	case string:
		return map[string]interface{}{"stringValue": t}
	case bool:
		return map[string]interface{}{"boolValue": t}
	case float64:
		// JSON numbers decode as float64.
		if math.Mod(t, 1) == 0 {
			return map[string]interface{}{"intValue": fmt.Sprintf("%d", int64(t))}
		}
		return map[string]interface{}{"doubleValue": t}
	case float32:
		return map[string]interface{}{"doubleValue": float64(t)}
	case int:
		return map[string]interface{}{"intValue": fmt.Sprintf("%d", t)}
	case int64:
		return map[string]interface{}{"intValue": fmt.Sprintf("%d", t)}
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return map[string]interface{}{"intValue": fmt.Sprintf("%d", i)}
		}
		if f, err := t.Float64(); err == nil {
			return map[string]interface{}{"doubleValue": f}
		}
		return map[string]interface{}{"stringValue": t.String()}
	default:
		return map[string]interface{}{"stringValue": fmt.Sprintf("%v", v)}
	}
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
