package sqliteexporter

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gotel/storage/sqlite"
)

func TestMetricRenderRejectsOversizedAndUnsupportedQueries(t *testing.T) {
	e := newTestExporter(t)
	defer e.shutdown(context.Background())
	metrics := make([]sqlite.MetricRecord, maxQueryLimit+1)
	for i := range metrics {
		metrics[i] = sqlite.MetricRecord{Name: "large.metric", Value: 1, Timestamp: 100, Tags: "{}"}
	}
	if err := e.store.InsertData(context.Background(), nil, metrics); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		url    string
		status int
	}{
		{"/render?target=large.metric&from=0&until=200", 422},
		{"/render?target=large.metric&from=0&until=0", 200},
		{"/render?target=sumSeries(*)&from=0", 400},
	} {
		w := httptest.NewRecorder()
		e.handleRenderMetrics(w, httptest.NewRequest("GET", tc.url, nil))
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.url, w.Code, w.Body.String())
		}
		if tc.status == 200 && w.Body.String() != "[]\n" {
			t.Fatalf("epoch-zero bound ignored: %s", w.Body.String())
		}
	}
}

func TestEnvironmentOverridesAreValidated(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"GOTEL_RETENTION", "-1h"}, {"GOTEL_RETENTION", "garbage"},
		{"GOTEL_QUERY_HOST", "127.0.0.1:3200"}, {"GOTEL_ALLOWED_ORIGINS", "*"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := newSQLiteExporter(&Config{}, nil); err == nil {
				t.Fatal("invalid environment accepted")
			}
		})
	}
}
