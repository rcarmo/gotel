package sqliteexporter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/gotel/storage/sqlite"
)

// maxQueryLimit is the maximum number of results returned by query endpoints.
const maxQueryLimit = 10000

// maxLoggedBodyBytes caps request body logging to avoid large allocations.
const maxLoggedBodyBytes = 64 * 1024

// clampLimit returns the given limit clamped to [1, maxQueryLimit].
// If limit <= 0 it returns the provided defaultLimit.
func clampLimit(limit, defaultLimit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxQueryLimit {
		return maxQueryLimit
	}
	return limit
}

func (e *sqliteExporter) writeJSON(w http.ResponseWriter, payload interface{}) {
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		e.logger.Debug("Failed to encode response", zap.Error(err))
	}
}

func (e *sqliteExporter) writeError(w http.ResponseWriter, msg string, err error, status int) {
	if status >= http.StatusInternalServerError {
		if err != nil {
			e.logger.Error(msg, zap.Error(err))
		} else {
			e.logger.Error(msg)
		}
	} else {
		if err != nil {
			e.logger.Warn(msg, zap.Error(err))
		} else {
			e.logger.Warn(msg)
		}
	}
	http.Error(w, msg, status)
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// corsMiddleware adds CORS headers to all responses.
// NOTE: The wildcard origin is intentional for dev/internal use and Grafana
// datasource compatibility. For production deployments exposed to the internet,
// consider restricting Access-Control-Allow-Origin via a reverse proxy or by
// adding a cors_allowed_origins config option.
func (e *sqliteExporter) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs all HTTP requests
func (e *sqliteExporter) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Read POST body for debug logging, then restore it.
		// Only perform the read if debug logging is enabled to avoid
		// unnecessary allocations on every request.
		var bodyStr string
		if r.Method == "POST" && r.Body != nil && e.logger.Core().Enabled(zap.DebugLevel) {
			bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxLoggedBodyBytes+1))
			if err == nil {
				if len(bodyBytes) > maxLoggedBodyBytes {
					bodyStr = string(bodyBytes[:maxLoggedBodyBytes]) + "... (truncated)"
				} else {
					bodyStr = string(bodyBytes)
				}
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}
		}

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Process request
		next.ServeHTTP(wrapped, r)

		// Log request details — body at Debug level to avoid leaking sensitive data
		e.logger.Info("HTTP request",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("query", r.URL.RawQuery),
			zap.Int("status", wrapped.statusCode),
			zap.Duration("duration", time.Since(start)),
			zap.String("remote_addr", r.RemoteAddr),
		)
		if bodyStr != "" {
			e.logger.Debug("HTTP request body",
				zap.String("path", r.URL.Path),
				zap.String("body", bodyStr),
			)
		}
	})
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

	// New endpoints for web UI
	mux.HandleFunc("/api/traces", e.handleListTraces)
	mux.HandleFunc("/api/spans", e.handleListSpans)
	mux.HandleFunc("/api/exceptions", e.handleListExceptions)

	// Graphite-compatible endpoints
	mux.HandleFunc("/render", e.handleRenderMetrics)
	mux.HandleFunc("/metrics/find", e.handleFindMetrics)

	// Status endpoints
	mux.HandleFunc("/api/status", e.handleStatus)
	mux.HandleFunc("/ready", e.handleReady)

	// Wrap mux with CORS and logging middleware
	handler := e.loggingMiddleware(e.corsMiddleware(mux))

	e.server.Handler = handler

	e.logger.Info("Starting query server", zap.Int("port", e.config.QueryPort))

	if err := e.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		e.logger.Error("Query server error", zap.Error(err))
	}
}

// handleGetTrace returns a single trace by ID
func (e *sqliteExporter) handleGetTrace(w http.ResponseWriter, r *http.Request) {
	traceID := strings.TrimPrefix(r.URL.Path, "/api/traces/")
	isV2 := false
	if strings.HasPrefix(r.URL.Path, "/api/v2/traces/") {
		traceID = strings.TrimPrefix(r.URL.Path, "/api/v2/traces/")
		isV2 = true
	}
	if traceID == "" {
		e.writeError(w, "trace_id required", nil, http.StatusBadRequest)
		return
	}

	spans, err := e.store.QueryTraceByID(r.Context(), traceID)
	if err != nil {
		e.writeError(w, "Failed to load trace", err, http.StatusInternalServerError)
		return
	}

	// Tempo returns OTLP JSON by default. We produce a best-effort OTLP-ish JSON
	// shape using the fields we persist.
	resourceSpans := groupSpansAsOTLPResourceSpans(spans)

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		// OTLP JSON-ish shape.
		"resourceSpans": resourceSpans,
		// Some clients/plugins historically expected Tempo's "batches" key.
		"batches": resourceSpans,
	}
	if isV2 {
		resp["trace"] = map[string]interface{}{
			"resourceSpans": resourceSpans,
			"batches":       resourceSpans,
		}
	}
	e.writeJSON(w, resp)
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
	limit = clampLimit(limit, 20)

	serviceName := strings.TrimSpace(q.Get("service"))
	spanName := strings.TrimSpace(q.Get("operation"))

	// Grafana's Tempo UI will often use '*' (or occasionally '.*') as an "All"
	// value. Treat these as "no filter" to avoid returning an empty result set.
	if serviceName == "*" || serviceName == ".*" {
		serviceName = ""
	}
	if spanName == "*" || spanName == ".*" {
		spanName = ""
	}

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
		e.writeError(w, "Failed to search traces", err, http.StatusInternalServerError)
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
	e.writeJSON(w, map[string]interface{}{
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
	e.writeJSON(w, map[string]interface{}{
		"tagNames": []string{"service.name", "span.name", "status"},
		"metrics":  map[string]interface{}{},
	})
}

func (e *sqliteExporter) handleSearchTagsV2(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	e.writeJSON(w, map[string]interface{}{
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
	tag = strings.TrimPrefix(tag, ".")

	// Only support service.name for now.
	if tag != "service.name" && tag != "resource.service.name" {
		e.writeError(w, "unsupported tag", nil, http.StatusNotFound)
		return
	}

	services, err := e.store.ListServices(r.Context())
	if err != nil {
		e.writeError(w, "Failed to list services", err, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	e.writeJSON(w, map[string]interface{}{
		"tagValues": services,
		"metrics":   map[string]interface{}{},
	})
}

func (e *sqliteExporter) handleSearchTagValuesV2(w http.ResponseWriter, r *http.Request) {
	tag := strings.TrimPrefix(r.URL.Path, "/api/v2/search/tag/")
	tag = strings.TrimSuffix(tag, "/values")
	tag = strings.TrimPrefix(tag, ".")

	if tag != "service.name" && tag != "resource.service.name" {
		e.writeError(w, "unsupported tag", nil, http.StatusNotFound)
		return
	}

	services, err := e.store.ListServices(r.Context())
	if err != nil {
		e.writeError(w, "Failed to list services", err, http.StatusInternalServerError)
		return
	}

	values := make([]map[string]interface{}, 0, len(services))
	for _, s := range services {
		values = append(values, map[string]interface{}{"type": "string", "value": s})
	}

	w.Header().Set("Content-Type", "application/json")
	e.writeJSON(w, map[string]interface{}{
		"tagValues": values,
		"metrics":   map[string]interface{}{},
	})
}

// handleListServices lists available services
func (e *sqliteExporter) handleListServices(w http.ResponseWriter, r *http.Request) {
	services, err := e.store.ListServices(r.Context())
	if err != nil {
		e.writeError(w, "Failed to list services", err, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	e.writeJSON(w, services)
}

// handleRenderMetrics returns metric data (Graphite-compatible)
func (e *sqliteExporter) handleRenderMetrics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	targets := q["target"]
	if len(targets) == 0 {
		if v := strings.TrimSpace(q.Get("target")); v != "" {
			targets = []string{v}
		}
	}
	if len(targets) == 0 && (r.Method == http.MethodPost || r.Method == http.MethodPut) {
		if err := r.ParseForm(); err != nil {
			e.writeError(w, "invalid form data", err, http.StatusBadRequest)
			return
		}
		if vs := r.Form["target"]; len(vs) > 0 {
			targets = append([]string(nil), vs...)
		} else if v := strings.TrimSpace(r.FormValue("target")); v != "" {
			targets = []string{v}
		}
	}
	allResults := make([]map[string]interface{}, 0)

	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}

		// Support a small subset of Graphite functions used in dashboards.
		// Handle nested functions by resolving inner functions first.
		var finalResults []map[string]interface{}
		var handled bool

		// Try aliasSub first (outer function)
		if inner, search, replace, ok := parseAliasSub(target); ok {
			// The inner part might itself be a function call
			var innerSeries map[string][]interface{}
			var err error

			// Check if inner is another function call
			if innerInner, idxs, ok2 := parseAliasByNode(inner); ok2 {
				innerSeries, err = e.queryMetricSeries(r.Context(), innerInner)
				if err != nil {
					e.writeError(w, "Failed to query metrics", err, http.StatusInternalServerError)
					return
				}
				// Apply aliasByNode first, then aliasSub
				for name, datapoints := range innerSeries {
					aliasedName := aliasByNode(name, idxs)
					finalName := aliasSub(aliasedName, search, replace)
					finalResults = append(finalResults, map[string]interface{}{
						"target":     finalName,
						"datapoints": datapoints,
					})
				}
			} else {
				// Inner is a regular metric pattern
				innerSeries, err = e.queryMetricSeries(r.Context(), inner)
				if err != nil {
					e.writeError(w, "Failed to query metrics", err, http.StatusInternalServerError)
					return
				}
				// Apply aliasSub directly
				for name, datapoints := range innerSeries {
					finalResults = append(finalResults, map[string]interface{}{
						"target":     aliasSub(name, search, replace),
						"datapoints": datapoints,
					})
				}
			}
			handled = true
		}

		// Try aliasByNode if not handled by aliasSub
		if !handled {
			if inner, idxs, ok := parseAliasByNode(target); ok {
				series, err := e.queryMetricSeries(r.Context(), inner)
				if err != nil {
					e.writeError(w, "Failed to query metrics", err, http.StatusInternalServerError)
					return
				}
				for name, datapoints := range series {
					finalResults = append(finalResults, map[string]interface{}{
						"target":     aliasByNode(name, idxs),
						"datapoints": datapoints,
					})
				}
				handled = true
			}
		}

		if handled {
			allResults = append(allResults, finalResults...)
			continue
		}

		series, err := e.queryMetricSeries(r.Context(), target)
		if err != nil {
			e.writeError(w, "Failed to query metrics", err, http.StatusInternalServerError)
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
	e.writeJSON(w, allResults)
}

// handleFindMetrics finds metric names (Graphite-compatible)
func (e *sqliteExporter) handleFindMetrics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("query"))
	if query == "" {
		query = strings.TrimSpace(q.Get("q"))
	}
	if query == "" && (r.Method == http.MethodPost || r.Method == http.MethodPut) {
		if err := r.ParseForm(); err != nil {
			e.writeError(w, "invalid form data", err, http.StatusBadRequest)
			return
		}
		query = strings.TrimSpace(r.FormValue("query"))
		if query == "" {
			query = strings.TrimSpace(r.FormValue("q"))
		}
	}
	if query == "" {
		// Grafana sometimes probes /metrics/find with an empty query during
		// dashboard/templating initialization. Graphite-compatible behaviour is to
		// return an empty list rather than a hard error.
		w.Header().Set("Content-Type", "application/json")
		e.writeJSON(w, []interface{}{})
		return
	}

	// Support aliasByNode(...) in find queries for template variables.
	// Handle nested functions by resolving inner functions first.
	var finalResult []map[string]interface{}
	var handled bool

	// Try aliasSub first (outer function)
	if inner, search, replace, ok := parseAliasSub(query); ok {
		// The inner part might itself be a function call
		var found []string
		var err error

		// Check if inner is another function call
		if innerInner, idxs, ok2 := parseAliasByNode(inner); ok2 {
			found, err = e.findMetricNodes(r.Context(), innerInner)
			if err != nil {
				e.writeError(w, "Failed to find metrics", err, http.StatusInternalServerError)
				return
			}
			// Apply aliasByNode first, then aliasSub
			for _, name := range found {
				aliasedName := aliasByNode(name, idxs)
				finalName := aliasSub(aliasedName, search, replace)
				finalResult = append(finalResult, map[string]interface{}{
					"text":          finalName,
					"id":            finalName,
					"expandable":    false,
					"allowChildren": false,
				})
			}
		} else {
			// Inner is a regular metric pattern
			found, err = e.findMetricNodes(r.Context(), inner)
			if err != nil {
				e.writeError(w, "Failed to find metrics", err, http.StatusInternalServerError)
				return
			}
			// Apply aliasSub directly
			for _, name := range found {
				finalResult = append(finalResult, map[string]interface{}{
					"text":          aliasSub(name, search, replace),
					"id":            aliasSub(name, search, replace),
					"expandable":    false,
					"allowChildren": false,
				})
			}
		}
		handled = true
	}

	// Try aliasByNode if not handled by aliasSub
	if !handled {
		if inner, idxs, ok := parseAliasByNode(query); ok {
			found, err := e.findMetricNodes(r.Context(), inner)
			if err != nil {
				e.writeError(w, "Failed to find metrics", err, http.StatusInternalServerError)
				return
			}
			for _, name := range found {
				alias := aliasByNode(name, idxs)
				finalResult = append(finalResult, map[string]interface{}{
					"text":          alias,
					"id":            alias,
					"expandable":    false,
					"allowChildren": false,
				})
			}
			handled = true
		}
	}

	if handled {
		w.Header().Set("Content-Type", "application/json")
		e.writeJSON(w, finalResult)
		return
	}

	found, err := e.findMetricNodes(r.Context(), query)
	if err != nil {
		e.writeError(w, "Failed to find metrics", err, http.StatusInternalServerError)
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
	e.writeJSON(w, result)
}

// handleStatus returns storage statistics
func (e *sqliteExporter) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats, err := e.store.Stats(r.Context())
	if err != nil {
		e.writeError(w, "Failed to load stats", err, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	e.writeJSON(w, stats)
}

// handleReady returns ready status
func (e *sqliteExporter) handleReady(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ready"))
}

// handleListTraces returns trace summaries
func (e *sqliteExporter) handleListTraces(w http.ResponseWriter, r *http.Request) {
	e.logger.Debug("Handling request for traces list")

	// Use SearchTraces to get aggregated trace summaries from the database
	traces, err := e.store.SearchTraces(r.Context(), sqlite.TraceSearchOptions{
		Limit: clampLimit(0, 1000),
	})
	if err != nil {
		e.writeError(w, "Failed to query traces", err, http.StatusInternalServerError)
		return
	}

	// Convert to JSON response format
	traceList := make([]map[string]interface{}, 0, len(traces))
	for _, t := range traces {
		traceList = append(traceList, map[string]interface{}{
			"trace_id":     t.TraceID,
			"span_name":    t.RootTraceName,
			"service_name": t.RootServiceName,
			"duration_ms":  t.DurationMs,
			"status_code":  t.StatusCode,
			"span_count":   t.SpanCount,
			"start_time":   t.StartTimeUnixNano,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	e.writeJSON(w, traceList)
}

// handleListSpans returns individual spans with filters
func (e *sqliteExporter) handleListSpans(w http.ResponseWriter, r *http.Request) {
	e.logger.Debug("Handling request for spans list")

	// Parse query parameters
	queryOptions := sqlite.SpanQueryOptions{
		Limit: 1000,
	}

	if serviceName := r.URL.Query().Get("service"); serviceName != "" {
		queryOptions.ServiceName = serviceName
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			queryOptions.Limit = clampLimit(limit, 1000)
		}
	}

	spans, err := e.store.QuerySpans(r.Context(), queryOptions)
	if err != nil {
		e.writeError(w, "Failed to query spans", err, http.StatusInternalServerError)
		return
	}
	if spans == nil {
		spans = []json.RawMessage{}
	}

	w.Header().Set("Content-Type", "application/json")
	e.writeJSON(w, spans)
}

// handleListExceptions returns span events and exceptions
func (e *sqliteExporter) handleListExceptions(w http.ResponseWriter, r *http.Request) {
	e.logger.Debug("Handling request for exceptions list")

	// Query spans with error status
	errorCode := 2
	errorSpans, err := e.store.QuerySpans(r.Context(), sqlite.SpanQueryOptions{
		StatusCode: &errorCode,
		Limit:      clampLimit(0, 1000),
	})
	if err != nil {
		e.writeError(w, "Failed to query error spans", err, http.StatusInternalServerError)
		return
	}

	// Convert error spans to exception format
	exceptions := make([]map[string]interface{}, 0)
	for _, spanRaw := range errorSpans {
		var span struct {
			TraceID           string `json:"trace_id"`
			SpanID            string `json:"span_id"`
			ServiceName       string `json:"service_name"`
			SpanName          string `json:"span_name"`
			StartTimeUnixNano int64  `json:"start_time_unix_nano"`
			Status            struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"status"`
			Events []struct {
				Name       string                 `json:"name"`
				Timestamp  int64                  `json:"timestamp"`
				Attributes map[string]interface{} `json:"attributes"`
			} `json:"events"`
		}

		if err := json.Unmarshal(spanRaw, &span); err != nil {
			continue
		}

		if span.Status.Code == 2 { // Error status
			exceptionCount := 0

			// Emit one exception entry per exception event, using the event timestamp.
			for _, event := range span.Events {
				if !strings.Contains(strings.ToLower(event.Name), "exception") {
					continue
				}

				timestampMs := event.Timestamp / 1000000
				if timestampMs == 0 {
					timestampMs = span.StartTimeUnixNano / 1000000
				}

				exception := map[string]interface{}{
					"trace_id":     span.TraceID,
					"span_id":      span.SpanID,
					"service_name": span.ServiceName,
					"span_name":    span.SpanName,
					"timestamp":    timestampMs,
				}

				if excType, ok := event.Attributes["exception.type"].(string); ok {
					exception["exception_type"] = excType
				}
				if excMessage, ok := event.Attributes["exception.message"].(string); ok {
					exception["message"] = excMessage
				}
				if excStack, ok := event.Attributes["exception.stacktrace"].(string); ok {
					exception["stack_trace"] = excStack
				}

				// Default severity if not set
				if _, hasSeverity := exception["severity"]; !hasSeverity {
					exception["severity"] = "critical"
				}

				exceptions = append(exceptions, exception)
				exceptionCount++
			}

			// If no exception events exist, emit a fallback entry using span start time.
			if exceptionCount == 0 {
				exception := map[string]interface{}{
					"trace_id":     span.TraceID,
					"span_id":      span.SpanID,
					"service_name": span.ServiceName,
					"span_name":    span.SpanName,
					"timestamp":    span.StartTimeUnixNano / 1000000,
				}
				if span.Status.Message != "" {
					exception["message"] = span.Status.Message
				}
				if _, hasSeverity := exception["severity"]; !hasSeverity {
					exception["severity"] = "critical"
				}
				exceptions = append(exceptions, exception)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	e.writeJSON(w, exceptions)
}

func (e *sqliteExporter) queryMetricSeries(ctx context.Context, target string) (map[string][]interface{}, error) {
	pattern := target
	namePattern := strings.Contains(pattern, "*") || strings.Contains(pattern, "?")

	// Calculate expected segment count from the pattern (Graphite * matches single segment only).
	// NOTE: We intentionally allow metrics with equal or more segments than the
	// pattern. This deviates from strict Graphite semantics (where * only matches
	// a single segment) but is required here because service/span names may
	// themselves contain dots (e.g. "azure_openai.completions"), which produce
	// additional segments beyond the pattern's expectation.
	expectedSegments := len(strings.Split(target, "."))

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
		// Filter: allow metrics with equal or more segments when using wildcards
		// This ensures * can match multi-segment operations (like azure_openai.completions)
		if namePattern && len(strings.Split(m.Name, ".")) < expectedSegments {
			continue
		}
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
