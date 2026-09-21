package sqliteexporter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestOTLPResourceScopeAndSpanFidelity(t *testing.T) {
	e := newTestExporter(t)
	defer e.shutdown(context.Background())
	td := ptrace.NewTraces()
	const ns = uint64(1790012345678901234)
	for instance := 0; instance < 2; instance++ {
		rs := td.ResourceSpans().AppendEmpty()
		rs.SetSchemaUrl("https://example.test/resource")
		rs.Resource().Attributes().PutStr("service.name", "same-service")
		rs.Resource().Attributes().PutInt("service.instance.id", int64(instance))
		rs.Resource().SetDroppedAttributesCount(3)
		for version := 0; version < 2; version++ {
			ss := rs.ScopeSpans().AppendEmpty()
			ss.SetSchemaUrl("https://example.test/scope")
			ss.Scope().SetName("same-scope")
			ss.Scope().SetVersion(fmt.Sprint(version))
			ss.Scope().Attributes().PutStr("scope-tag", "preserved")
			s := ss.Spans().AppendEmpty()
			s.SetTraceID(pcommon.TraceID{1})
			s.SetSpanID(pcommon.SpanID{byte(instance + 1), byte(version + 1)})
			s.SetStartTimestamp(pcommon.Timestamp(ns))
			s.SetEndTimestamp(pcommon.Timestamp(ns + 123456))
			s.SetKind(ptrace.SpanKindServer)
			s.Status().SetCode(ptrace.StatusCodeError)
			s.SetFlags(1)
			s.TraceState().FromRaw("vendor=value")
			s.Attributes().PutInt("large", 9223372036854775807)
			s.Attributes().PutEmptyMap("nested").PutEmptySlice("items").AppendEmpty().SetInt(123)
			event := s.Events().AppendEmpty()
			event.SetName("exception")
			event.SetTimestamp(pcommon.Timestamp(ns + 17))
			event.Attributes().PutStr("exception.message", "failure")
			link := s.Links().AppendEmpty()
			link.SetTraceID(pcommon.TraceID{2})
			link.SetSpanID(pcommon.SpanID{3})
			link.TraceState().FromRaw("other=value")
			link.Attributes().PutInt("large", 9223372036854775807)
			link.SetFlags(1)
		}
	}
	if err := e.pushTraces(context.Background(), td); err != nil {
		t.Fatal(err)
	}
	raw, err := e.store.QueryTraceByID(context.Background(), pcommon.TraceID{1}.String())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]interface{}{"resourceSpans": groupSpansAsOTLPResourceSpans(raw)})
	if err != nil {
		t.Fatal(err)
	}
	var u ptrace.JSONUnmarshaler
	got, err := u.UnmarshalTraces(payload)
	if err != nil {
		t.Fatalf("%v: %s", err, payload)
	}
	if got.ResourceSpans().Len() != 2 {
		t.Fatalf("distinct resources merged: %s", payload)
	}
	for i := 0; i < 2; i++ {
		rs := got.ResourceSpans().At(i)
		if rs.ScopeSpans().Len() != 2 || rs.SchemaUrl() != "https://example.test/resource" || rs.Resource().DroppedAttributesCount() != 3 {
			t.Fatalf("resource/scope metadata lost: %s", payload)
		}
		for j := 0; j < 2; j++ {
			ss := rs.ScopeSpans().At(j)
			s := ss.Spans().At(0)
			if ss.Scope().Version() == "" || ss.Scope().Attributes().Len() != 1 || ss.SchemaUrl() != "https://example.test/scope" {
				t.Fatal("scope metadata lost")
			}
			if s.StartTimestamp() != pcommon.Timestamp(ns) || s.EndTimestamp() != pcommon.Timestamp(ns+123456) || s.Events().At(0).Timestamp() != pcommon.Timestamp(ns+17) {
				t.Fatal("timestamp precision lost")
			}
			if s.Status().Code() != ptrace.StatusCodeError || s.TraceState().AsRaw() != "vendor=value" || s.Flags() != 1 {
				t.Fatal("span status/state/flags lost")
			}
			large, _ := s.Attributes().Get("large")
			if large.Int() != 9223372036854775807 {
				t.Fatal("integer precision lost")
			}
			nested, _ := s.Attributes().Get("nested")
			if nested.Type() != pcommon.ValueTypeMap {
				t.Fatal("structured attributes lost")
			}
			if s.Links().Len() != 1 || s.Links().At(0).TraceState().AsRaw() != "other=value" || s.Links().At(0).Flags() != 1 {
				t.Fatal("link metadata lost")
			}
		}
	}
}

func TestRawTraceSpansAreNotGlobalSample(t *testing.T) {
	e := newTestExporter(t)
	defer e.shutdown(context.Background())
	for i := 0; i < 125; i++ {
		raw := []byte(fmt.Sprintf(`{"trace_id":"selected","span_id":"%d","start_time_unix_nano":%d}`, i, i))
		if err := e.store.InsertSpan(context.Background(), raw); err != nil {
			t.Fatal(err)
		}
	}
	w := httptest.NewRecorder()
	e.queryHandler().ServeHTTP(w, httptest.NewRequest("GET", "/api/traces/selected/spans", nil))
	var spans []json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &spans); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(spans) != 125 {
		t.Fatalf("status=%d spans=%d", w.Code, len(spans))
	}
}

func TestMetricDiscoveryUsesNamesNotSamples(t *testing.T) {
	e := newTestExporter(t)
	defer e.shutdown(context.Background())
	for i := 0; i < 2100; i++ {
		if err := e.store.InsertMetric(context.Background(), "old.metric", 1, 100, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.store.InsertMetric(context.Background(), "new.metric", 1, 200, nil); err != nil {
		t.Fatal(err)
	}
	nodes, err := e.findMetricNodes(context.Background(), "*.metric")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("newer series hidden: %v", nodes)
	}
}

func TestOriginPolicy(t *testing.T) {
	e := &sqliteExporter{config: &Config{AllowedOrigins: []string{"https://allowed.test"}}, logger: zap.NewNop()}
	h := e.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for _, tc := range []struct {
		origin string
		want   int
	}{{"", 200}, {"http://example.com", 200}, {"https://evil.test", 403}, {"https://allowed.test", 200}, {"null", 403}} {
		r := httptest.NewRequest("GET", "http://example.com/ready", nil)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want || w.Header().Get("Access-Control-Allow-Origin") == "*" {
			t.Errorf("origin %q: %d", tc.origin, w.Code)
		}
	}
}

type closingBody struct {
	io.Reader
	closed bool
}

func (b *closingBody) Close() error { b.closed = true; return nil }
func TestDebugBodyClosePreserved(t *testing.T) {
	logger := zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(io.Discard), zap.DebugLevel))
	e := &sqliteExporter{logger: logger}
	body := &closingBody{Reader: strings.NewReader(strings.Repeat("x", maxLoggedBodyBytes+100))}
	r := httptest.NewRequest("POST", "/render", nil)
	r.Body = body
	h := e.loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil || len(data) != maxLoggedBodyBytes+100 {
			t.Fatalf("body changed: %d %v", len(data), err)
		}
		r.Body.Close()
	}))
	h.ServeHTTP(httptest.NewRecorder(), r)
	if !body.closed {
		t.Fatal("original body not closed")
	}
}

func TestGraphiteTimeRangesAndForm(t *testing.T) {
	e := newTestExporter(t)
	defer e.shutdown(context.Background())
	e.store.InsertMetric(context.Background(), "metric", 1, 100, nil)
	e.store.InsertMetric(context.Background(), "metric", 2, 200, nil)
	for _, tc := range []struct {
		body   string
		status int
	}{{"target=metric&from=150&until=250", 200}, {"target=metric&from=garbage", 400}, {"target=metric&from=250&until=150", 400}} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/render", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		e.handleRenderMetrics(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.body, w.Code, w.Body.String())
		}
		if tc.status == 200 && !bytes.Contains(w.Body.Bytes(), []byte(`[[2,200]]`)) {
			t.Fatalf("wrong range: %s", w.Body.String())
		}
	}
	now := time.Unix(2000000000, 0)
	for _, tc := range []struct {
		value string
		want  int64
	}{{"now", now.Unix()}, {"-1h", now.Unix() - 3600}, {"-2d", now.Unix() - 172800}, {"150", 150}} {
		got, err := parseGraphiteTime(tc.value, now)
		if err != nil || got != tc.want {
			t.Fatalf("%s: %d %v", tc.value, got, err)
		}
	}
}
