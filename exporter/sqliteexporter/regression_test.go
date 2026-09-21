package sqliteexporter

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gotel/storage/sqlite"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestReviewOTLPTimestampRoundTrip(t *testing.T) {
	raw := json.RawMessage(`{"trace_id":"0102030405060708090a0b0c0d0e0f10","span_id":"0102030405060708","span_name":"test","kind":"Internal","start_time_unix_nano":1790012345678901234,"end_time_unix_nano":1790012345679901234,"status":{"code":0}}`)
	payload, _ := json.Marshal(map[string]interface{}{"resourceSpans": groupSpansAsOTLPResourceSpans([]json.RawMessage{raw})})
	var u ptrace.JSONUnmarshaler
	_, err := u.UnmarshalTraces(payload)
	if err != nil {
		t.Fatalf("actual OTLP parser rejected response: %v; response=%s", err, payload)
	}
}
func TestReviewDebugLoggingPreservesBody(t *testing.T) {
	logger := zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(io.Discard), zap.DebugLevel))
	e := &sqliteExporter{logger: logger}
	body := strings.Repeat("a", maxLoggedBodyBytes+100)
	var got []byte
	h := e.loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got, _ = io.ReadAll(r.Body) }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/render", strings.NewReader(body)))
	if len(got) != len(body) {
		t.Fatalf("body truncated: wanted %d bytes, received %d", len(body), len(got))
	}
}
func TestReviewNegativeConfigRejected(t *testing.T) {
	for _, cfg := range []*Config{{Retention: -time.Hour}, {CleanupInterval: -time.Second}, {QueryPort: 65536}} {
		if err := cfg.Validate(); err == nil {
			t.Errorf("invalid configuration accepted: %+v", cfg)
		}
	}
}
func TestReviewRenderHonorsRange(t *testing.T) {
	store, err := sqlite.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.InsertMetric(context.Background(), "test.metric", 1, 100, nil)
	store.InsertMetric(context.Background(), "test.metric", 2, 200, nil)
	e := &sqliteExporter{store: store, logger: zap.NewNop()}
	w := httptest.NewRecorder()
	e.handleRenderMetrics(w, httptest.NewRequest("GET", "/render?target=test.metric&from=150&until=250", nil))
	var result []struct {
		Datapoints [][]float64 `json:"datapoints"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	if len(result) != 1 || len(result[0].Datapoints) != 1 {
		t.Fatalf("out-of-range point included: %s", w.Body.String())
	}
}
func TestReviewStartupDetectsOccupiedPort(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	e, err := newSQLiteExporter(&Config{DBPath: filepath.Join(t.TempDir(), "test.db"), QueryPort: l.Addr().(*net.TCPAddr).Port}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	err = e.start(context.Background(), nil)
	defer e.shutdown(context.Background())
	if err == nil {
		t.Fatal("start returned success although query port already occupied")
	}
}
