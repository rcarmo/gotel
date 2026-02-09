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
	totalDuration float64
	errorCount    int64
}

// newSQLiteExporter creates a new SQLite exporter
func newSQLiteExporter(config *Config, logger *zap.Logger) (*sqliteExporter, error) {
	if err := config.applyEnvironmentOverrides(); err != nil {
		return nil, err
	}
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
		e.server = &http.Server{
			Addr:              fmt.Sprintf(":%d", e.config.QueryPort),
			ReadHeaderTimeout: 10 * time.Second,
			MaxHeaderBytes:    1 << 20, // 1 MB
		}
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
					spanJSON, err := e.spanToJSON(span, resource, ss.Scope())
					if err != nil {
						e.logger.Error("Failed to marshal span JSON", zap.Error(err))
						continue
					}
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

					duration := float64(span.EndTimestamp().AsTime().Sub(span.StartTimestamp().AsTime()).Nanoseconds()) / 1e6
					if duration < 0 {
						duration = 0
					}

					if span.Status().Code() == ptrace.StatusCodeError {
						agg.errorCount++
						e.logger.Debug("Found error span", zap.String("span_name", spanNameRaw), zap.Float64("duration_ms", duration))
					}

					// Accumulate duration for all spans to avoid bias
					agg.totalDuration += duration
				}
			}

			// Generate metrics
			if e.config.SendMetrics {
				for spanNameMetric, agg := range spanAggs {
					prefix := e.buildPrefix(serviceNameMetric, spanNameMetric)
					tags := map[string]string{"service": serviceNameRaw, "span": agg.rawSpanName}
					tagsJSON, err := json.Marshal(tags)
					if err != nil {
						e.logger.Error("Failed to marshal metric tags", zap.Error(err))
						continue
					}

					metrics = append(metrics, sqlite.MetricRecord{
						Name:      fmt.Sprintf("%s.span_count", prefix),
						Value:     float64(agg.count),
						Timestamp: timestamp,
						Tags:      string(tagsJSON),
					})

					// Calculate average duration
					if agg.count > 0 {
						avgDuration := agg.totalDuration / float64(agg.count)
						e.logger.Debug("Average duration calculated",
							zap.String("span_name", agg.rawSpanName),
							zap.Float64("avg_duration_ms", avgDuration))
						metrics = append(metrics, sqlite.MetricRecord{
							Name:      fmt.Sprintf("%s.duration_ms", prefix),
							Value:     avgDuration,
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

	// Batch insert spans and metrics atomically
	if len(spanJSONs) > 0 || len(metrics) > 0 {
		if err := e.store.InsertData(ctx, spanJSONs, metrics); err != nil {
			return fmt.Errorf("failed to insert data: %w", err)
		}
	}

	e.logger.Debug("Stored traces",
		zap.Int("spans", len(spanJSONs)),
		zap.Int("metrics", len(metrics)))

	return nil
}

// spanToJSON converts a span to JSON for storage
func (e *sqliteExporter) spanToJSON(span ptrace.Span, resource pcommon.Resource, scope pcommon.InstrumentationScope) ([]byte, error) {
	// Extract service name from resource
	serviceName := "unknown"
	if serviceAttr, ok := resource.Attributes().Get("service.name"); ok {
		serviceName = serviceAttr.Str()
	}

	// Calculate duration in milliseconds (float for precision)
	durationMs := float64(span.EndTimestamp().AsTime().Sub(span.StartTimestamp().AsTime()).Nanoseconds()) / 1e6
	if durationMs < 0 {
		durationMs = 0
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
		"duration_ms":          durationMs,
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
				evAttrs := make(map[string]interface{})
				ev.Attributes().Range(func(k string, v pcommon.Value) bool {
					evAttrs[k] = v.AsRaw()
					return true
				})
				eventData["attributes"] = evAttrs
			}
			events = append(events, eventData)
		}
		data["events"] = events
	}

	return json.Marshal(data)
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
				if e.cleanupCtx.Err() != nil {
					// Context cancelled during shutdown, don't log as error
					return
				}
				e.logger.Error("Cleanup failed", zap.Error(err))
			} else if deleted > 0 {
				e.logger.Info("Cleanup completed", zap.Int64("deleted", deleted))
			}
		}
	}
}
