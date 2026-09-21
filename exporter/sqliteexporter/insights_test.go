package sqliteexporter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gotel/storage/sqlite"
)

func mustExporterInsightSpan(t *testing.T, traceID, spanID, parentID, service, operation, kind, version string, startSec int64, durationMs int64, status int, attrs map[string]interface{}, errorType string, extras map[string]interface{}) []byte {
	t.Helper()
	resource := map[string]interface{}{}
	if version != "" {
		resource["service.version"] = version
	}
	span := map[string]interface{}{
		"trace_id":             traceID,
		"span_id":              spanID,
		"parent_span_id":       parentID,
		"service_name":         service,
		"span_name":            operation,
		"kind":                 kind,
		"start_time_unix_nano": startSec * 1e9,
		"end_time_unix_nano":   startSec*1e9 + durationMs*1e6,
		"duration_ms":          durationMs,
		"status":               map[string]interface{}{"code": status},
		"resource":             resource,
		"attributes":           attrs,
	}
	if errorType != "" {
		span["events"] = []map[string]interface{}{{
			"name": "exception",
			"attributes": map[string]interface{}{
				"exception.type": errorType,
				"secret":         "discard me",
			},
		}}
	}
	for k, v := range extras {
		span[k] = v
	}
	data, err := json.Marshal(span)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustInsertExporterSpan(t *testing.T, e *sqliteExporter, raw []byte) {
	t.Helper()
	if err := e.store.InsertSpan(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
}

func TestParseInsightQueryRejectsInvalidInputs(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		want string
	}{
		{"invalid-time", "/api/insights?from=-1", "invalid from"},
		{"duplicate-time", "/api/insights?from=1&from=2", "duplicate from"},
		{"bad-range", "/api/insights?from=10&to=10", "time range must be positive"},
		{"bad-scope", "/api/insights?scope=spans", "scope must be requests or all"},
		{"bad-status", "/api/insights?status=ok", "status must be error or empty"},
		{"bad-duration", "/api/insights?min_duration_ms=-1", "invalid min_duration_ms"},
		{"duration-order", "/api/insights?min_duration_ms=2&max_duration_ms=1", "minimum duration exceeds maximum"},
		{"missing-value", "/api/insights?attribute=tenant", "attribute and value must be supplied together"},
		{"missing-attribute", "/api/insights?value=acme", "attribute and value must be supplied together"},
		{"oversize-filter", "/api/insights?service=" + strings.Repeat("x", 1025), "filter exceeds 1024 bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseInsightQuery(httptest.NewRequest(http.MethodGet, tc.url, nil))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestAccumulatorSummaryUsesNearestRankHistogramAndErrorFractions(t *testing.T) {
	var a accumulator
	durations := []float64{9, 10, 49, 50, 99, 100, 499, 500, 999, 1000}
	for i, ms := range durations {
		status := 0
		if i == 1 || i == 5 || i == 9 {
			status = 2
		}
		a.add(sqlite.Observation{
			TraceID: fmt.Sprintf("trace-%d", i),
			Start:   0,
			End:     int64(math.Round(ms * 1e6)),
			Status:  status,
		})
	}

	d := a.summary(20)
	if d.Count != 10 || d.ErrorCount != 3 {
		t.Fatalf("count/error mismatch: %#v", d)
	}
	if d.DurationSum != 3315 {
		t.Fatalf("sum mismatch: %#v", d)
	}
	if d.P50 != 99 || d.P95 != 1000 || d.P99 != 1000 {
		t.Fatalf("percentiles mismatch: %#v", d)
	}
	if d.Rate != 0.5 || d.ErrorRate != 0.3 {
		t.Fatalf("rate mismatch: %#v", d)
	}
	wantHist := [6]int{1, 2, 2, 2, 2, 1}
	if d.Histogram != wantHist {
		t.Fatalf("histogram mismatch: got %#v want %#v", d.Histogram, wantHist)
	}
	if a.trace != "trace-9" {
		t.Fatalf("expected slowest trace chosen, got %q", a.trace)
	}
}

func TestCollectInsightsTracksPreviousWindowErrorRecurrenceAndVersions(t *testing.T) {
	e := newTestExporter(t)
	defer e.shutdown(context.Background())

	mustInsertExporterSpan(t, e, mustExporterInsightSpan(t,
		"prev-trace", "0000000000000001", "", "api", "GET /orders", "server", "1.0.0", 50, 20, 2, nil, "TimeoutError", nil,
	))
	mustInsertExporterSpan(t, e, mustExporterInsightSpan(t,
		"cur-trace-a", "0000000000000002", "", "api", "GET /orders", "server", "1.0.0", 110, 40, 2, nil, "TimeoutError", nil,
	))
	mustInsertExporterSpan(t, e, mustExporterInsightSpan(t,
		"cur-trace-b", "0000000000000003", "", "api", "GET /orders", "server", "1.1.0", 120, 80, 2, nil, "TimeoutError", nil,
	))

	out, err := e.collectInsights(context.Background(), sqlite.InsightQuery{From: 100, To: 160, Scope: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Comparison {
		t.Fatal("expected previous-window comparison to be available")
	}
	if len(out.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %#v", out.Operations)
	}
	op := out.Operations[0]
	if op.Previous == nil || *op.Previous != 20 {
		t.Fatalf("previous p95 mismatch: %#v", op)
	}
	if op.Change == nil || *op.Change != 300 {
		t.Fatalf("change mismatch: %#v", op)
	}
	if len(out.Errors) != 1 {
		t.Fatalf("expected 1 error group, got %#v", out.Errors)
	}
	errRow := out.Errors[0]
	if errRow.Previous == nil || *errRow.Previous != 1 {
		t.Fatalf("previous error count mismatch: %#v", errRow)
	}
	if got := strings.Join(errRow.Versions, ","); got != "1.0.0,1.1.0" {
		t.Fatalf("versions mismatch: %#v", errRow)
	}
}

func TestCollectInsightsIgnoresPreviousWindowOverflow(t *testing.T) {
	e := newTestExporter(t)
	defer e.shutdown(context.Background())

	batch := make([][]byte, 0, sqlite.InsightSpanLimit+2)
	for i := 0; i < sqlite.InsightSpanLimit+1; i++ {
		batch = append(batch, mustExporterInsightSpan(t,
			fmt.Sprintf("prev-%05d", i), fmt.Sprintf("%016x", i), "", "api", "GET /busy", "server", "1.0.0", 50, 1, 0, nil, "", nil,
		))
	}
	if err := e.store.InsertData(context.Background(), batch, nil); err != nil {
		t.Fatal(err)
	}
	mustInsertExporterSpan(t, e, mustExporterInsightSpan(t,
		"cur-trace", "00000000000000ff", "", "api", "GET /busy", "server", "1.0.0", 110, 5, 0, nil, "", nil,
	))

	out, err := e.collectInsights(context.Background(), sqlite.InsightQuery{From: 100, To: 160, Scope: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Comparison {
		t.Fatalf("comparison should be unavailable when prior window overflows: %#v", out)
	}
	if out.Summary.Count != 1 {
		t.Fatalf("current window should still succeed: %#v", out.Summary)
	}
}

func TestAggregateAgentsUsesAliasPrecedenceWithoutDoubleCounting(t *testing.T) {
	rows := aggregateAgents([]sqlite.Observation{
		{
			TraceID:   "trace-tool",
			Service:   "assistant",
			Operation: "fallback-op",
			Status:    2,
			Start:     0,
			End:       50 * 1e6,
			Attributes: map[string]interface{}{
				"gen_ai.response.model":          "gpt-4.1",
				"gen_ai.request.model":           "should-not-win",
				"llm.model_name":                 "also-ignored",
				"gen_ai.provider.name":           "openai",
				"gen_ai.system":                  "ignored-provider",
				"gen_ai.operation.name":          "tool",
				"gen_ai.tool.name":               "search",
				"gen_ai.usage.input_tokens":      10.0,
				"gen_ai.usage.prompt_tokens":     99.0,
				"llm.usage.prompt_tokens":        77.0,
				"gen_ai.usage.output_tokens":     "3",
				"gen_ai.usage.completion_tokens": 33.0,
				"llm.usage.completion_tokens":    22.0,
				"gen_ai.cost.total":              0.4,
				"llm.cost.total":                 9.9,
				"gen_ai.request.retry_count":     1.0,
				"retry.count":                    7,
			},
		},
		{
			TraceID:   "trace-tool-2",
			Service:   "assistant",
			Operation: "unused",
			Status:    0,
			Start:     0,
			End:       20 * 1e6,
			Attributes: map[string]interface{}{
				"gen_ai.tool.name":           "search",
				"gen_ai.provider.name":       "openai",
				"gen_ai.usage.cost_usd":      "oops",
				"gen_ai.request.retry_count": "bad",
			},
		},
	}, 10)
	if len(rows) != 2 {
		t.Fatalf("expected 2 groups, got %#v", rows)
	}
	if rows[0].Model != "Unknown" || rows[0].Operation != "execute_tool" || rows[0].Count != 1 || rows[0].ToolCalls != 1 {
		t.Fatalf("unknown-model tool group mismatch: %#v", rows[0])
	}
	if rows[1].Model != "gpt-4.1" || rows[1].Provider != "openai" || rows[1].Operation != "tool" {
		t.Fatalf("alias precedence mismatch: %#v", rows[1])
	}
	if rows[1].Input != 10 || rows[1].Output != 3 || rows[1].TokenSamples != 1 {
		t.Fatalf("token aliases double-counted or wrong precedence: %#v", rows[1])
	}
	if rows[1].Cost != 0.4 || rows[1].CostSamples != 1 || rows[1].Retries != 1 || rows[1].RetrySamples != 1 {
		t.Fatalf("cost/retry aliases mismatch: %#v", rows[1])
	}
	if rows[1].Errors != 1 || rows[1].ToolFailures != 1 || rows[1].P95 != 50 {
		t.Fatalf("error/tool aggregation mismatch: %#v", rows[1])
	}
}

func TestSafeBundleSpanAllowlistPreservesIDsAndNanosecondStrings(t *testing.T) {
	raw := []byte(`{
		"trace_id":"0123456789abcdef0123456789abcdef",
		"span_id":"0123456789abcdef",
		"parent_span_id":"0000000000000000",
		"service_name":"checkout",
		"span_name":"POST /pay",
		"kind":"server",
		"start_time_unix_nano":1790012345678901234,
		"end_time_unix_nano":1790012345679901234,
		"duration_ms":1.25,
		"status":{"code":2,"message":"keep hidden"},
		"resource":{"service.name":"checkout","service.version":"1.2.3","deployment.environment":"prod","secret":"hide","nested":{"token":"hide"}},
		"scope":{"name":"danger","version":"1.0.0"},
		"attributes":{
			"gen_ai.response.model":"gpt-4.1",
			"gen_ai.usage.input_tokens":123,
			"prompt":"remove",
			"response":"remove",
			"secret":"remove"
		},
		"events":[{"name":"exception","attributes":{"exception.type":"TimeoutError","prompt":"hide"}}],
		"links":[
			{"trace_id":"fedcba9876543210fedcba9876543210","span_id":"0011223344556677","attributes":{"secret":"hide"}},
			{"trace_id":"UPPERCASE0123456789abcdef01234567","span_id":"0011223344556677"}
		]
	}`)
	original := append([]byte(nil), raw...)

	safe, err := safeBundleSpan(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(original) {
		t.Fatal("safeBundleSpan mutated its input")
	}
	if safe["start_time_unix_nano"] != "1790012345678901234" || safe["end_time_unix_nano"] != "1790012345679901234" {
		t.Fatalf("nanosecond precision should be emitted as strings: %#v", safe)
	}
	if _, ok := safe["events"]; ok {
		t.Fatalf("events should be stripped: %#v", safe)
	}
	if _, ok := safe["scope"]; ok {
		t.Fatalf("scope should be stripped: %#v", safe)
	}
	attrs := safe["attributes"].(map[string]interface{})
	if len(attrs) != 2 || attrs["gen_ai.response.model"] != "gpt-4.1" || attrs["gen_ai.usage.input_tokens"].(float64) != 123 {
		t.Fatalf("attribute allowlist mismatch: %#v", attrs)
	}
	resource := safe["resource"].(map[string]string)
	if len(resource) != 3 || resource["service.name"] != "checkout" || resource["service.version"] != "1.2.3" || resource["deployment.environment"] != "prod" {
		t.Fatalf("resource allowlist mismatch: %#v", resource)
	}
	links := safe["links"].([]map[string]string)
	if len(links) != 1 || links[0]["trace_id"] != "fedcba9876543210fedcba9876543210" {
		t.Fatalf("link validation mismatch: %#v", links)
	}
}

func TestNativeQueryRejectsWrongMethod(t *testing.T) {
	e := newTestExporter(t)
	defer e.shutdown(context.Background())

	h := e.nativeQuery(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodPost, "/api/insights", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
	if w.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("allow header mismatch: %#v", w.Header())
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("native headers missing: %#v", w.Header())
	}
}

func TestHandleInvestigationValidatesIDsAndExportsCompleteTrace(t *testing.T) {
	e := newTestExporter(t)
	defer e.shutdown(context.Background())

	h := e.nativeQuery(e.handleInvestigation)
	bad := httptest.NewRecorder()
	h(bad, httptest.NewRequest(http.MethodGet, "/api/investigation?trace_id=not-hex", nil))
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "trace IDs must be unique lowercase 32-digit hex") {
		t.Fatalf("expected trace-id validation error, got %d %s", bad.Code, bad.Body.String())
	}

	traceID := "0123456789abcdef0123456789abcdef"
	now := time.Now().Unix()
	for i := 0; i < 101; i++ {
		mustInsertExporterSpan(t, e, mustExporterInsightSpan(t,
			traceID,
			fmt.Sprintf("%016x", i+1),
			"0000000000000000",
			"checkout",
			"POST /pay",
			"server",
			"1.2.3",
			now,
			1,
			0,
			map[string]interface{}{"prompt": "top-secret", "gen_ai.response.model": "gpt-4.1"},
			"",
			map[string]interface{}{"scope": map[string]interface{}{"name": "strip-me"}},
		))
	}

	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodGet, "/api/investigation?trace_id="+traceID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", w.Code, w.Body.String())
	}
	if disp := w.Header().Get("Content-Disposition"); !strings.Contains(disp, "gotel-investigation.json") {
		t.Fatalf("missing attachment header: %q", disp)
	}
	var bundle struct {
		Traces []struct {
			TraceID string                   `json:"trace_id"`
			Spans   []map[string]interface{} `json:"spans"`
		} `json:"traces"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Traces) != 1 || len(bundle.Traces[0].Spans) != 101 {
		t.Fatalf("expected complete exported trace, got %#v", bundle)
	}
	if _, ok := bundle.Traces[0].Spans[0]["scope"]; ok {
		t.Fatalf("scope metadata leaked: %#v", bundle.Traces[0].Spans[0])
	}
	attrs := bundle.Traces[0].Spans[0]["attributes"].(map[string]interface{})
	if _, ok := attrs["prompt"]; ok || attrs["gen_ai.response.model"] != "gpt-4.1" {
		t.Fatalf("attribute allowlist broken: %#v", attrs)
	}
}

func TestHandleInsightsReturnsExplicitLimitErrorAtTwentyThousandSpans(t *testing.T) {
	e := newTestExporter(t)
	defer e.shutdown(context.Background())

	batch := make([][]byte, 0, sqlite.InsightSpanLimit+2)
	for i := 0; i < sqlite.InsightSpanLimit+1; i++ {
		batch = append(batch, mustExporterInsightSpan(t,
			fmt.Sprintf("trace-%05d", i), fmt.Sprintf("%016x", i), "", "svc", "op", "server", "", 100, 1, 0, nil, "", nil,
		))
	}
	if err := e.store.InsertData(context.Background(), batch, nil); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	e.nativeQuery(e.handleInsights)(w, httptest.NewRequest(http.MethodGet, "/api/insights?from=99&to=101&scope=all", nil))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(sqlite.ErrInsightLimit.Error())) {
		t.Fatalf("expected explicit limit message, got %s", w.Body.String())
	}
}

func TestInternalErrorsAndMissingTokenSides(t *testing.T) {
	e := newTestExporter(t)
	defer e.shutdown(context.Background())
	mustInsertExporterSpan(t, e, mustExporterInsightSpan(t, "0123456789abcdef0123456789abcdef", "0000000000000001", "0000000000000002", "agent", "chat", "internal", "v2", 110, 50, 2, map[string]interface{}{"gen_ai.usage.input_tokens": 12, "gen_ai.usage.cost_usd": true}, "ToolError", nil))
	out, err := e.collectInsights(context.Background(), sqlite.InsightQuery{From: 100, To: 160, Scope: "requests"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Summary.Count != 0 || len(out.Errors) != 1 {
		t.Fatalf("internal error hidden by request scope: %+v", out)
	}
	if len(out.Agents) != 1 || out.Agents[0].InputSamples != 1 || out.Agents[0].OutputSamples != 0 || out.Agents[0].CostSamples != 0 {
		t.Fatalf("unknown usage or boolean cost misinterpreted: %+v", out.Agents)
	}
}

func TestInvestigationRedactsAttributeFilterValue(t *testing.T) {
	e := newTestExporter(t)
	defer e.shutdown(context.Background())
	id := "0123456789abcdef0123456789abcdef"
	mustInsertExporterSpan(t, e, mustExporterInsightSpan(t, id, "0000000000000001", "", "svc", "op", "server", "v1", 110, 1, 1, map[string]interface{}{"private": "sensitive-filter-value"}, "", nil))
	w := httptest.NewRecorder()
	e.nativeQuery(e.handleInvestigation)(w, httptest.NewRequest("GET", "/api/investigation?from=100&to=160&attribute=private&value=sensitive-filter-value&trace_id="+id, nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "sensitive-filter-value") || !strings.Contains(w.Body.String(), "[REDACTED]") {
		t.Fatalf("unsafe export: %d %s", w.Code, w.Body.String())
	}
}
