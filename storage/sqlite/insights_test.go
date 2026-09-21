package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"testing"
)

func mustInsightSpan(t *testing.T, traceID, spanID, parentID, service, operation, kind, version string, startSec, durationMs int64, status int, attrs map[string]interface{}, errorType string) []byte {
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
		"status":               map[string]interface{}{"code": status},
		"resource":             resource,
		"attributes":           attrs,
	}
	if errorType != "" {
		span["events"] = []map[string]interface{}{{
			"name": "exception",
			"attributes": map[string]interface{}{
				"exception.type": errorType,
			},
		}}
	}
	data, err := json.Marshal(span)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustInsertInsightSpan(t *testing.T, store *Store, raw []byte) {
	t.Helper()
	if err := store.InsertSpan(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
}

func TestReadObservationsRejectsQueriesOverTwentyThousandSpans(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	for i := 0; i < InsightSpanLimit+1; i++ {
		mustInsertInsightSpan(t, store, mustInsightSpan(t,
			"limit-trace", fmt.Sprintf("span-limit-%05d", i), "", "svc", "op", "server", "", 100, 1, 0, nil, "",
		))
	}

	_, err := store.ReadObservations(context.Background(), InsightQuery{From: 99, To: 101, Scope: "all"})
	if !errors.Is(err, ErrInsightLimit) {
		t.Fatalf("expected ErrInsightLimit, got %v", err)
	}
}

func TestExploreMatchesSameSpanButReturnsFullTraceSummary(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	mustInsertInsightSpan(t, store, mustInsightSpan(t,
		"trace-match", "root-1", "", "edge", "GET /checkout", "server", "1.0.0", 50, 10, 0, nil, "",
	))
	mustInsertInsightSpan(t, store, mustInsightSpan(t,
		"trace-match", "child-1", "root-1", "payments", "charge-card", "internal", "1.2.3", 150, 20, 2,
		map[string]interface{}{"tenant": "acme"}, "TimeoutError",
	))

	mustInsertInsightSpan(t, store, mustInsightSpan(t,
		"trace-other", "root-2", "", "edge", "GET /other", "server", "1.0.0", 140, 5, 0,
		map[string]interface{}{"tenant": "acme"}, "",
	))
	mustInsertInsightSpan(t, store, mustInsightSpan(t,
		"trace-other", "child-2", "root-2", "payments", "charge-card", "internal", "9.9.9", 160, 5, 0,
		map[string]interface{}{"tenant": "other"}, "",
	))

	traces, err := store.Explore(context.Background(), InsightQuery{
		From:      100,
		To:        200,
		Service:   "payments",
		Operation: "charge-card",
		Status:    "error",
		Attribute: "tenant",
		Value:     "acme",
		Scope:     "all",
	}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d: %#v", len(traces), traces)
	}
	got := traces[0]
	if got.TraceID != "trace-match" {
		t.Fatalf("matched wrong trace: %#v", got)
	}
	if got.RootServiceName != "edge" || got.RootTraceName != "GET /checkout" {
		t.Fatalf("root summary should come from the full trace, got %#v", got)
	}
	if got.SpanCount != 2 {
		t.Fatalf("expected full trace span count, got %#v", got)
	}
	if got.StartTimeUnixNano != 50*1e9 || got.DurationMs != 100020 {
		t.Fatalf("expected cross-window timing preserved, got %#v", got)
	}
}

func TestInsightServicesIncludesStaleServicesOutsideWindow(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	mustInsertInsightSpan(t, store, mustInsightSpan(t,
		"trace-old", "old-1", "", "stale-svc", "old-op", "server", "0.9.0", 10, 5, 0, nil, "",
	))
	mustInsertInsightSpan(t, store, mustInsightSpan(t,
		"trace-new", "new-1", "", "fresh-svc", "new-op", "server", "1.0.0", 150, 5, 0, nil, "",
	))

	services, err := store.InsightServices(context.Background(), InsightQuery{From: 100, To: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Service < services[j].Service })
	if services[0].Service != "fresh-svc" || !services[0].InWindow || services[0].LastSeen != 150 {
		t.Fatalf("fresh service mismatch: %#v", services[0])
	}
	if services[1].Service != "stale-svc" || services[1].InWindow || services[1].LastSeen != 10 {
		t.Fatalf("stale service mismatch: %#v", services[1])
	}
}

func TestReadBundleTracesReturnsCompleteTraceBeyondOneHundredSpans(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	for i := 0; i < 101; i++ {
		mustInsertInsightSpan(t, store, mustInsightSpan(t,
			"trace-bundle", fmt.Sprintf("bundle-%03d", i), "", "svc", "op", "server", "", int64(i), 1, 0, nil, "",
		))
	}

	bundles, err := store.ReadBundleTraces(context.Background(), []string{"trace-bundle"})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(bundles["trace-bundle"]); got != 101 {
		t.Fatalf("expected all 101 spans, got %d", got)
	}
}
