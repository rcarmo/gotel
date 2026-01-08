package graphiteexporter

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

// graphiteExporter exports traces to Graphite as metrics
type graphiteExporter struct {
	config *Config
	logger *zap.Logger
	conn   net.Conn
	mu     sync.Mutex
}

// newGraphiteExporter creates a new Graphite exporter
func newGraphiteExporter(config *Config, logger *zap.Logger) (*graphiteExporter, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &graphiteExporter{
		config: config,
		logger: logger,
	}, nil
}

// start establishes connection to Graphite
func (e *graphiteExporter) start(ctx context.Context, host component.Host) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	conn, err := net.DialTimeout("tcp", e.config.Endpoint, e.config.Timeout)
	if err != nil {
		return fmt.Errorf("failed to connect to Graphite at %s: %w", e.config.Endpoint, err)
	}

	e.conn = conn
	e.logger.Info("Connected to Graphite", zap.String("endpoint", e.config.Endpoint))
	return nil
}

// shutdown closes the connection to Graphite
func (e *graphiteExporter) shutdown(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.conn != nil {
		err := e.conn.Close()
		e.conn = nil
		return err
	}
	return nil
}

// pushTraces converts traces to Graphite metrics and sends them
func (e *graphiteExporter) pushTraces(ctx context.Context, td ptrace.Traces) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.config.SendMetrics {
		return nil
	}

	if e.conn == nil {
		// Try to reconnect
		conn, err := net.DialTimeout("tcp", e.config.Endpoint, e.config.Timeout)
		if err != nil {
			return fmt.Errorf("failed to reconnect to Graphite: %w", err)
		}
		e.conn = conn
	}

	metrics := e.tracesToMetrics(td)

	for _, metric := range metrics {
		if _, err := fmt.Fprintln(e.conn, metric); err != nil {
			e.conn.Close()
			e.conn = nil
			return fmt.Errorf("failed to write metric to Graphite: %w", err)
		}
	}

	e.logger.Debug("Sent metrics to Graphite", zap.Int("count", len(metrics)))
	return nil
}

// tracesToMetrics converts traces to Graphite plaintext protocol format
func (e *graphiteExporter) tracesToMetrics(td ptrace.Traces) []string {
	var metrics []string
	timestamp := time.Now().Unix()

	resourceSpans := td.ResourceSpans()
	for i := 0; i < resourceSpans.Len(); i++ {
		rs := resourceSpans.At(i)
		resource := rs.Resource()

		// Extract service name from resource attributes
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

				// Count spans
				spanCounts[spanName]++

				// Sum durations (in milliseconds), clamping negative values to zero
				duration := span.EndTimestamp().AsTime().Sub(span.StartTimestamp().AsTime()).Milliseconds()
				if duration < 0 {
					duration = 0
				}
				spanDurations[spanName] += duration

				// Count errors
				if span.Status().Code() == ptrace.StatusCodeError {
					spanErrors[spanName]++
				}
			}

			// Generate metrics for this scope
			for spanName, count := range spanCounts {
				prefix := e.buildPrefix(serviceName, spanName)

				// Span count metric
				metrics = append(metrics, e.formatMetric(
					fmt.Sprintf("%s.span_count", prefix),
					count,
					timestamp,
					map[string]string{"service": serviceName, "span": spanName},
				))

				// Average duration metric
				if count > 0 {
					avgDuration := spanDurations[spanName] / count
					metrics = append(metrics, e.formatMetric(
						fmt.Sprintf("%s.duration_ms", prefix),
						avgDuration,
						timestamp,
						map[string]string{"service": serviceName, "span": spanName},
					))
				}

				// Error count metric (only emit if there are errors)
				if errorCount := spanErrors[spanName]; errorCount > 0 {
					metrics = append(metrics, e.formatMetric(
						fmt.Sprintf("%s.error_count", prefix),
						errorCount,
						timestamp,
						map[string]string{"service": serviceName, "span": spanName},
					))
				}
			}
		}
	}

	return metrics
}

// buildPrefix constructs the metric prefix
func (e *graphiteExporter) buildPrefix(serviceName, spanName string) string {
	parts := []string{e.config.Prefix}

	if e.config.Namespace != "" {
		parts = append(parts, e.config.Namespace)
	}

	parts = append(parts, serviceName, spanName)

	return strings.Join(parts, ".")
}

// formatMetric formats a metric in Graphite plaintext or tagged format
func (e *graphiteExporter) formatMetric(name string, value int64, timestamp int64, tags map[string]string) string {
	if e.config.TagSupport && len(tags) > 0 {
		// Tagged format: metric;tag1=value1;tag2=value2 value timestamp
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
		return fmt.Sprintf("%s;%s %d %d", name, strings.Join(tagParts, ";"), value, timestamp)
	}
	// Plain format: metric value timestamp
	return fmt.Sprintf("%s %d %d", name, value, timestamp)
}

// sanitizeMetricName replaces invalid characters in metric names
func sanitizeMetricName(name string) string {
	// Replace spaces, slashes, and other invalid characters with underscores
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
