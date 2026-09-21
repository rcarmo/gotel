package sqliteexporter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gotel/storage/sqlite"
)

type distribution struct {
	Count       int     `json:"count"`
	ErrorCount  int     `json:"error_count"`
	DurationSum float64 `json:"duration_sum_ms"`
	Rate        float64 `json:"rate_per_second"`
	ErrorRate   float64 `json:"error_rate"`
	P50         float64 `json:"p50_ms"`
	P95         float64 `json:"p95_ms"`
	P99         float64 `json:"p99_ms"`
	Histogram   [6]int  `json:"histogram"`
}
type accumulator struct {
	durations []float64
	errors    int
	trace     string
	max       float64
}

func (a *accumulator) add(o sqlite.Observation) {
	d := math.Max(0, float64(o.End-o.Start)/1e6)
	a.durations = append(a.durations, d)
	if o.Status == 2 {
		a.errors++
	}
	if a.trace == "" || d > a.max {
		a.max = d
		a.trace = o.TraceID
	}
}
func (a *accumulator) summary(seconds float64) distribution {
	d := distribution{Count: len(a.durations), ErrorCount: a.errors}
	if d.Count == 0 {
		return d
	}
	sort.Float64s(a.durations)
	for _, v := range a.durations {
		d.DurationSum += v
		i := sort.SearchFloat64s([]float64{10, 50, 100, 500, 1000}, v)
		for i < 5 && v >= []float64{10, 50, 100, 500, 1000}[i] {
			i++
		}
		d.Histogram[i]++
	}
	percentile := func(p float64) float64 { return a.durations[int(math.Ceil(float64(d.Count)*p))-1] }
	d.P50 = percentile(.5)
	d.P95 = percentile(.95)
	d.P99 = percentile(.99)
	d.Rate = float64(d.Count) / seconds
	d.ErrorRate = float64(a.errors) / float64(d.Count)
	return d
}

type insightBucket struct {
	distribution
	From  int64  `json:"from"`
	To    int64  `json:"to"`
	Trace string `json:"trace_id"`
}
type insightOperation struct {
	distribution
	Service   string   `json:"service"`
	Operation string   `json:"operation"`
	Trace     string   `json:"trace_id"`
	Previous  *float64 `json:"previous_p95_ms"`
	Change    *float64 `json:"change_percent"`
}
type insightError struct {
	Service   string   `json:"service"`
	Operation string   `json:"operation"`
	Type      string   `json:"type"`
	Count     int      `json:"count"`
	First     float64  `json:"first_seen"`
	Last      float64  `json:"last_seen"`
	Trace     string   `json:"trace_id"`
	Previous  *int     `json:"previous_count"`
	Versions  []string `json:"versions"`
}
type agentRow struct {
	Service       string  `json:"service"`
	Model         string  `json:"model"`
	Operation     string  `json:"operation"`
	Provider      string  `json:"provider"`
	Count         int     `json:"count"`
	Errors        int     `json:"error_count"`
	ToolCalls     int     `json:"tool_calls"`
	ToolFailures  int     `json:"tool_failures"`
	Retries       float64 `json:"retries"`
	RetrySamples  int     `json:"retry_samples"`
	Input         float64 `json:"input_tokens"`
	Output        float64 `json:"output_tokens"`
	TokenSamples  int     `json:"token_samples"`
	InputSamples  int     `json:"input_token_samples"`
	OutputSamples int     `json:"output_token_samples"`
	Cost          float64 `json:"cost_usd"`
	CostSamples   int     `json:"cost_samples"`
	P50           float64 `json:"p50_ms"`
	P95           float64 `json:"p95_ms"`
	Trace         string  `json:"trace_id"`
	agg           accumulator
}
type insightsResponse struct {
	From       int64                   `json:"from"`
	To         int64                   `json:"to"`
	Scope      string                  `json:"scope"`
	Comparison bool                    `json:"comparison_available"`
	Summary    distribution            `json:"summary"`
	Buckets    []insightBucket         `json:"buckets"`
	Operations []insightOperation      `json:"operations"`
	Errors     []insightError          `json:"errors"`
	Services   []sqlite.InsightService `json:"services"`
	Agents     []agentRow              `json:"agents"`
	Scanned    int                     `json:"scanned_spans"`
	MaxSpans   int                     `json:"max_spans"`
}

func parseInsightQuery(r *http.Request) (sqlite.InsightQuery, error) {
	v := r.URL.Query()
	to := time.Now().Unix() + 1
	q := sqlite.InsightQuery{From: to - 3600, To: to, Scope: "requests", Service: v.Get("service"), Operation: v.Get("operation"), Status: v.Get("status"), Attribute: v.Get("attribute"), Value: v.Get("value")}
	for key, dst := range map[string]*int64{"from": &q.From, "to": &q.To} {
		if raw, exists := v[key]; exists {
			if len(raw) != 1 {
				return q, fmt.Errorf("duplicate %s", key)
			}
			n, e := strconv.ParseInt(raw[0], 10, 64)
			if e != nil || n < 0 || n > 9223372035 {
				return q, fmt.Errorf("invalid %s: use Unix seconds", key)
			}
			*dst = n
		}
	}
	if q.To <= q.From || q.To-q.From > 7*86400 {
		return q, errors.New("time range must be positive and at most 7 days")
	}
	if x := v.Get("scope"); x != "" {
		q.Scope = x
	}
	if q.Scope != "requests" && q.Scope != "all" {
		return q, errors.New("scope must be requests or all")
	}
	if q.Status != "" && q.Status != "error" {
		return q, errors.New("status must be error or empty")
	}
	for key, dst := range map[string]**float64{"min_duration_ms": &q.MinDuration, "max_duration_ms": &q.MaxDuration} {
		if raw, exists := v[key]; exists {
			if len(raw) != 1 {
				return q, fmt.Errorf("duplicate %s", key)
			}
			n, e := strconv.ParseFloat(raw[0], 64)
			if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > float64(math.MaxInt64)/1e6 {
				return q, fmt.Errorf("invalid %s", key)
			}
			*dst = &n
		}
	}
	if q.MinDuration != nil && q.MaxDuration != nil && *q.MinDuration > *q.MaxDuration {
		return q, errors.New("minimum duration exceeds maximum")
	}
	if (q.Attribute != "") != v.Has("value") {
		return q, errors.New("attribute and value must be supplied together")
	}
	for _, s := range []string{q.Service, q.Operation, q.Attribute, q.Value} {
		if len(s) > 1024 {
			return q, errors.New("filter exceeds 1024 bytes")
		}
	}
	return q, nil
}

func requestObservation(o sqlite.Observation) bool {
	return strings.EqualFold(o.Kind, "server") || strings.EqualFold(o.Kind, "consumer") || o.ParentID == "" || o.ParentID == "0000000000000000"
}
func selectedObservations(in []sqlite.Observation, scope string) []sqlite.Observation {
	if scope == "all" {
		return in
	}
	out := []sqlite.Observation{}
	for _, o := range in {
		if requestObservation(o) {
			out = append(out, o)
		}
	}
	return out
}

type operationKey struct{ service, operation string }
type errorKey struct{ service, operation, kind string }

func errorIdentity(o sqlite.Observation) errorKey {
	k := o.ErrorType
	if k == "" {
		k = "Error status"
	}
	return errorKey{o.Service, o.Operation, k}
}
func uniqueSorted(values []string, v string) []string {
	if v != "" {
		for _, x := range values {
			if x == v {
				return values
			}
		}
		values = append(values, v)
		sort.Strings(values)
	}
	return values
}

func (e *sqliteExporter) collectInsights(ctx context.Context, q sqlite.InsightQuery) (insightsResponse, error) {
	out := insightsResponse{From: q.From, To: q.To, Scope: q.Scope, MaxSpans: sqlite.InsightSpanLimit, Buckets: []insightBucket{}, Operations: []insightOperation{}, Errors: []insightError{}, Agents: []agentRow{}, Services: []sqlite.InsightService{}}
	raw, err := e.store.ReadObservations(ctx, q)
	if err != nil {
		return out, err
	}
	out.Scanned = len(raw)
	current := selectedObservations(raw, q.Scope)
	previous := []sqlite.Observation{}
	previousRaw := []sqlite.Observation{}
	prevQ := q
	prevQ.From = q.From - (q.To - q.From)
	prevQ.To = q.From
	if prevQ.From >= 0 {
		p, err := e.store.ReadObservations(ctx, prevQ)
		if err != nil && !errors.Is(err, sqlite.ErrInsightLimit) {
			return out, err
		}
		if err == nil {
			out.Comparison = true
			previousRaw = p
			previous = selectedObservations(p, q.Scope)
		}
	}
	prevOps := map[operationKey]*accumulator{}
	prevErrors := map[errorKey]int{}
	for _, o := range previous {
		k := operationKey{o.Service, o.Operation}
		if prevOps[k] == nil {
			prevOps[k] = &accumulator{}
		}
		prevOps[k].add(o)
	}
	for _, o := range previousRaw {
		if o.Status == 2 {
			prevErrors[errorIdentity(o)]++
		}
	}
	width := int64(60)
	for _, candidate := range []int64{1, 5, 10, 30, 60, 300, 900, 1800, 3600, 10800, 21600} {
		width = candidate
		if (q.To-q.From+candidate-1)/candidate <= 120 {
			break
		}
	}
	bucketAcc := []accumulator{}
	// UTC-aligned buckets clipped to the requested window; rate uses actual width.
	for start := q.From / width * width; start < q.To; start += width {
		from, to := start, start+width
		if from < q.From {
			from = q.From
		}
		if to > q.To {
			to = q.To
		}
		out.Buckets = append(out.Buckets, insightBucket{From: from, To: to})
		bucketAcc = append(bucketAcc, accumulator{})
	}
	ops := map[operationKey]*accumulator{}
	errs := map[errorKey]*insightError{}
	var total accumulator
	for _, o := range current {
		total.add(o)
		idx := (o.Start/1e9 - q.From/width*width) / width
		bucketAcc[idx].add(o)
		k := operationKey{o.Service, o.Operation}
		if ops[k] == nil {
			ops[k] = &accumulator{}
		}
		ops[k].add(o)
	}
	// Error groups include internal failures even when request metrics exclude them.
	for _, o := range raw {
		if o.Status == 2 {
			key := errorIdentity(o)
			x := errs[key]
			if x == nil {
				x = &insightError{Service: o.Service, Operation: o.Operation, Type: key.kind, First: float64(o.Start) / 1e9, Versions: []string{}}
				errs[key] = x
			}
			x.Count++
			x.Last = float64(o.Start) / 1e9
			x.Trace = o.TraceID
			x.Versions = uniqueSorted(x.Versions, o.Version)
		}
	}
	seconds := float64(q.To - q.From)
	out.Summary = total.summary(seconds)
	for i := range out.Buckets {
		b := &out.Buckets[i]
		b.distribution = bucketAcc[i].summary(float64(b.To - b.From))
		b.Trace = bucketAcc[i].trace
	}
	for k, a := range ops {
		x := insightOperation{distribution: a.summary(seconds), Service: k.service, Operation: k.operation, Trace: a.trace}
		if out.Comparison && prevOps[k] != nil {
			p := prevOps[k].summary(seconds).P95
			x.Previous = &p
			if p > 0 {
				delta := (x.P95 - p) / p * 100
				x.Change = &delta
			}
		}
		out.Operations = append(out.Operations, x)
	}
	sort.Slice(out.Operations, func(i, j int) bool {
		a, b := out.Operations[i], out.Operations[j]
		if a.P95 == b.P95 {
			if a.Service == b.Service {
				return a.Operation < b.Operation
			}
			return a.Service < b.Service
		}
		return a.P95 > b.P95
	})
	for k, x := range errs {
		if out.Comparison {
			n := prevErrors[k]
			x.Previous = &n
		}
		out.Errors = append(out.Errors, *x)
	}
	sort.Slice(out.Errors, func(i, j int) bool {
		a, b := out.Errors[i], out.Errors[j]
		if a.Count == b.Count {
			return a.Service+"\x00"+a.Operation+"\x00"+a.Type < b.Service+"\x00"+b.Operation+"\x00"+b.Type
		}
		return a.Count > b.Count
	})
	out.Services, err = e.store.InsightServices(ctx, q)
	if err != nil {
		return out, err
	}
	versions := map[string][]string{}
	for _, o := range raw {
		versions[o.Service] = uniqueSorted(versions[o.Service], o.Version)
	}
	for i := range out.Services {
		if v := versions[out.Services[i].Service]; v != nil {
			out.Services[i].Versions = v
		}
	}
	out.Agents = aggregateAgents(raw, seconds)
	return out, nil
}
func attrString(a map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if s, ok := a[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
func attrNumber(a map[string]interface{}, keys ...string) (float64, bool) {
	for _, k := range keys {
		v, ok := a[k]
		if !ok {
			continue
		}
		var n float64
		switch x := v.(type) {
		case float64:
			n = x
		case json.Number:
			var err error
			n, err = x.Float64()
			if err != nil {
				continue
			}
		case string:
			var err error
			n, err = strconv.ParseFloat(x, 64)
			if err != nil {
				continue
			}
		default:
			continue
		}
		if n >= 0 && !math.IsNaN(n) && !math.IsInf(n, 0) && n <= 1e15 {
			return n, true
		}
	}
	return 0, false
}
func aggregateAgents(raw []sqlite.Observation, seconds float64) []agentRow {
	type key struct{ service, model, op, provider string }
	groups := map[key]*agentRow{}
	for _, o := range raw {
		a := o.Attributes
		if len(a) == 0 {
			continue
		}
		model := attrString(a, "gen_ai.response.model", "gen_ai.request.model", "llm.model_name")
		provider := attrString(a, "gen_ai.provider.name", "gen_ai.system")
		op := attrString(a, "gen_ai.operation.name")
		tool := attrString(a, "gen_ai.tool.name") != "" || op == "execute_tool" || op == "tool"
		isAgent := model != "" || provider != "" || op != "" || tool
		for k := range a {
			if strings.HasPrefix(k, "gen_ai.") || strings.HasPrefix(k, "llm.") {
				isAgent = true
			}
		}
		if !isAgent {
			continue
		}
		if model == "" {
			model = "Unknown"
		}
		if op == "" {
			if tool {
				op = "execute_tool"
			} else {
				op = o.Operation
			}
		}
		k := key{o.Service, model, op, provider}
		x := groups[k]
		if x == nil {
			x = &agentRow{Service: o.Service, Model: model, Operation: op, Provider: provider}
			groups[k] = x
		}
		x.Count++
		if o.Status == 2 {
			x.Errors++
		}
		if tool {
			x.ToolCalls++
			if o.Status == 2 {
				x.ToolFailures++
			}
		}
		input, hasIn := attrNumber(a, "gen_ai.usage.input_tokens", "gen_ai.usage.prompt_tokens", "llm.usage.prompt_tokens")
		output, hasOut := attrNumber(a, "gen_ai.usage.output_tokens", "gen_ai.usage.completion_tokens", "llm.usage.completion_tokens")
		if hasIn {
			x.InputSamples++
		}
		if hasOut {
			x.OutputSamples++
		}
		if hasIn || hasOut {
			x.TokenSamples++
			x.Input += input
			x.Output += output
		}
		if cost, ok := attrNumber(a, "gen_ai.usage.cost_usd", "gen_ai.cost.total", "llm.cost.total"); ok {
			x.Cost += cost
			x.CostSamples++
		}
		if retries, ok := attrNumber(a, "gen_ai.request.retry_count", "retry.count"); ok {
			x.Retries += retries
			x.RetrySamples++
		}
		x.agg.add(o)
	}
	out := []agentRow{}
	for _, x := range groups {
		d := x.agg.summary(seconds)
		x.P50 = d.P50
		x.P95 = d.P95
		x.Trace = x.agg.trace
		out = append(out, *x)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		return a.Service+"\x00"+a.Provider+"\x00"+a.Model+"\x00"+a.Operation < b.Service+"\x00"+b.Provider+"\x00"+b.Model+"\x00"+b.Operation
	})
	return out
}

func (e *sqliteExporter) insightError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "Failed to query insights"
	if errors.Is(err, sqlite.ErrInsightLimit) {
		status = http.StatusUnprocessableEntity
		message = err.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		status = http.StatusGatewayTimeout
		message = "Query timed out; narrow the time range"
	}
	e.writeError(w, message, err, status)
}
func (e *sqliteExporter) handleInsights(w http.ResponseWriter, r *http.Request) {
	q, err := parseInsightQuery(r)
	if err != nil {
		e.writeError(w, err.Error(), nil, 400)
		return
	}
	out, err := e.collectInsights(r.Context(), q)
	if err != nil {
		e.insightError(w, err)
		return
	}
	e.writeJSON(w, out)
}
func (e *sqliteExporter) handleExplore(w http.ResponseWriter, r *http.Request) {
	q, err := parseInsightQuery(r)
	if err != nil {
		e.writeError(w, err.Error(), nil, 400)
		return
	}
	limit := 100
	if s := r.URL.Query().Get("limit"); s != "" {
		limit, err = strconv.Atoi(s)
		if err != nil || limit < 1 || limit > 200 {
			e.writeError(w, "limit must be 1..200", nil, 400)
			return
		}
	}
	traces, err := e.store.Explore(r.Context(), q, limit+1)
	if err != nil {
		e.insightError(w, err)
		return
	}
	more := len(traces) > limit
	if more {
		traces = traces[:limit]
	}
	list := make([]map[string]interface{}, 0, len(traces))
	for _, t := range traces {
		list = append(list, map[string]interface{}{"trace_id": t.TraceID, "service_name": t.RootServiceName, "span_name": t.RootTraceName, "duration_ms": t.DurationMs, "status_code": t.StatusCode, "span_count": t.SpanCount, "start_time": t.StartTimeUnixNano})
	}
	e.writeJSON(w, map[string]interface{}{"traces": list, "has_more": more})
}

var traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var spanIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

const redactionPolicy = "Allowlist v1: messages, stacks, events, prompts/responses, arbitrary attributes, attribute-filter values, baggage and scope metadata omitted. Service/operation/model/version labels, exception types and IDs remain; review before sharing."

func safeBundleSpan(raw json.RawMessage) (map[string]interface{}, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var source map[string]interface{}
	if err := dec.Decode(&source); err != nil {
		return nil, err
	}
	out := map[string]interface{}{}
	for _, key := range []string{"trace_id", "span_id", "parent_span_id", "service_name", "span_name", "kind"} {
		if v, ok := source[key].(string); ok {
			out[key] = v
		}
	}
	for _, key := range []string{"start_time_unix_nano", "end_time_unix_nano"} {
		n, ok := source[key].(json.Number)
		if !ok {
			return nil, fmt.Errorf("invalid timestamp in stored span")
		}
		if _, err := n.Int64(); err != nil {
			return nil, err
		}
		out[key] = n.String()
	}
	if d, ok := source["duration_ms"].(json.Number); ok {
		out["duration_ms"] = d
	} else {
		out["duration_ms"] = 0
	}
	status := map[string]interface{}{"code": 0}
	if s, ok := source["status"].(map[string]interface{}); ok {
		if code, ok := s["code"].(json.Number); ok {
			status["code"] = code
		}
	}
	out["status"] = status
	resource := map[string]string{}
	if r, ok := source["resource"].(map[string]interface{}); ok {
		for _, key := range []string{"service.name", "service.version", "deployment.environment", "deployment.environment.name"} {
			if v, ok := r[key].(string); ok {
				resource[key] = v
			}
		}
	}
	out["resource"] = resource
	attrs := map[string]interface{}{}
	if a, ok := source["attributes"].(map[string]interface{}); ok {
		for _, key := range sqlite.AgentAttributeKeys {
			if v, ok := a[key]; ok {
				isLabel := key == "gen_ai.system" || key == "gen_ai.provider.name" || key == "gen_ai.request.model" || key == "gen_ai.response.model" || key == "gen_ai.operation.name" || key == "gen_ai.tool.name" || key == "llm.model_name"
				if isLabel {
					if s, ok := v.(string); ok {
						attrs[key] = s
					}
				} else if n, ok := attrNumber(map[string]interface{}{key: v}, key); ok {
					attrs[key] = n
				}
			}
		}
	}
	out["attributes"] = attrs
	links := []map[string]string{}
	if ls, ok := source["links"].([]interface{}); ok {
		for _, l := range ls {
			if m, ok := l.(map[string]interface{}); ok {
				t, _ := m["trace_id"].(string)
				s, _ := m["span_id"].(string)
				if traceIDPattern.MatchString(t) && spanIDPattern.MatchString(s) {
					links = append(links, map[string]string{"trace_id": t, "span_id": s})
				}
			}
		}
	}
	out["links"] = links
	return out, nil
}
func (e *sqliteExporter) handleInvestigation(w http.ResponseWriter, r *http.Request) {
	q, err := parseInsightQuery(r)
	if err != nil {
		e.writeError(w, err.Error(), nil, 400)
		return
	}
	ids := r.URL.Query()["trace_id"]
	if len(ids) < 1 || len(ids) > 20 {
		e.writeError(w, "select 1..20 trace IDs", nil, 400)
		return
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !traceIDPattern.MatchString(id) || seen[id] {
			e.writeError(w, "trace IDs must be unique lowercase 32-digit hex", nil, 400)
			return
		}
		seen[id] = true
	}
	spans, err := e.store.ReadBundleTraces(r.Context(), ids)
	if err != nil {
		if strings.HasPrefix(err.Error(), "trace not found:") {
			e.writeError(w, err.Error(), nil, 404)
		} else {
			e.insightError(w, err)
		}
		return
	}
	insights, err := e.collectInsights(r.Context(), q)
	if err != nil {
		e.insightError(w, err)
		return
	}
	traces := []map[string]interface{}{}
	for _, id := range ids {
		safe := []map[string]interface{}{}
		for _, raw := range spans[id] {
			s, err := safeBundleSpan(raw)
			if err != nil {
				e.insightError(w, err)
				return
			}
			safe = append(safe, s)
		}
		traces = append(traces, map[string]interface{}{"trace_id": id, "spans": safe})
	}
	// Query filters can contain secrets too. Keep the key for context, never its value.
	if q.Attribute != "" {
		q.Value = "[REDACTED]"
	}
	bundle := map[string]interface{}{"format": "gotel-investigation", "version": 1, "created_at": time.Now().UTC().Format(time.RFC3339), "redaction": redactionPolicy, "filters": q, "traces": traces, "insights": insights}
	// Encode before headers so failed/oversized exports are never successful partial files.
	payload, err := json.Marshal(bundle)
	if err != nil {
		e.insightError(w, err)
		return
	}
	if len(payload) > sqlite.BundleByteLimit {
		e.insightError(w, sqlite.ErrInsightLimit)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="gotel-investigation.json"`)
	w.Write(payload)
}

func (e *sqliteExporter) nativeQuery(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", 405)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		handler(w, r.WithContext(ctx))
	}
}
