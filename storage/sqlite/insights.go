package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const InsightSpanLimit = 20000
const BundleByteLimit = 32 << 20

var ErrInsightLimit = errors.New("query exceeds investigation limits; narrow the time range or filters")

// InsightQuery matches all predicates on the SAME span. Windows are half-open.
type InsightQuery struct {
	From        int64    `json:"from"`
	To          int64    `json:"to"`
	Service     string   `json:"service,omitempty"`
	Operation   string   `json:"operation,omitempty"`
	Status      string   `json:"status,omitempty"`
	MinDuration *float64 `json:"min_duration_ms,omitempty"`
	MaxDuration *float64 `json:"max_duration_ms,omitempty"`
	Attribute   string   `json:"attribute,omitempty"`
	Value       string   `json:"value,omitempty"`
	Scope       string   `json:"scope"`
}

// Observation is an intentionally small projection: no messages or arbitrary payloads.
type Observation struct {
	TraceID, SpanID, ParentID, Service, Operation, Kind, Version, ErrorType string
	Start, End                                                              int64
	Status                                                                  int
	Attributes                                                              map[string]interface{}
}

var AgentAttributeKeys = []string{
	"gen_ai.system", "gen_ai.provider.name", "gen_ai.request.model", "gen_ai.response.model", "gen_ai.operation.name", "gen_ai.tool.name",
	"gen_ai.usage.input_tokens", "gen_ai.usage.output_tokens", "gen_ai.usage.prompt_tokens", "gen_ai.usage.completion_tokens",
	"llm.usage.prompt_tokens", "llm.usage.completion_tokens", "llm.model_name",
	"gen_ai.usage.cost_usd", "gen_ai.cost.total", "llm.cost.total", "gen_ai.request.retry_count", "retry.count",
}

func insightWhere(q InsightQuery, scope bool) (string, []interface{}) {
	where := "start_time_unix_nano >= ? AND start_time_unix_nano < ?"
	args := []interface{}{q.From * 1000000000, q.To * 1000000000}
	if q.Service != "" {
		where += " AND service_name = ?"
		args = append(args, q.Service)
	}
	if q.Operation != "" {
		where += " AND span_name = ?"
		args = append(args, q.Operation)
	}
	if q.Status == "error" {
		where += " AND status_code = 2"
	}
	if q.MinDuration != nil {
		where += " AND MAX(0,duration_ns) >= ?"
		args = append(args, *q.MinDuration*1e6)
	}
	if q.MaxDuration != nil {
		where += " AND MAX(0,duration_ns) <= ?"
		args = append(args, *q.MaxDuration*1e6)
	}
	if q.Attribute != "" {
		where += ` AND EXISTS (SELECT 1 FROM json_each(spans.data, '$.attributes') a WHERE a.key = ? AND CAST(a.value AS TEXT) = ?)`
		args = append(args, q.Attribute, q.Value)
	}
	if scope && q.Scope != "all" {
		where += ` AND (lower(json_extract(data,'$.kind')) IN ('server','consumer') OR parent_span_id IS NULL OR parent_span_id IN ('','0000000000000000'))`
	}
	return where, args
}

func (s *Store) ReadObservations(ctx context.Context, q InsightQuery) ([]Observation, error) {
	where, args := insightWhere(q, false)
	keys := make([]string, len(AgentAttributeKeys))
	for i, k := range AgentAttributeKeys {
		keys[i] = "'" + k + "'"
	}
	query := `SELECT COALESCE(trace_id,''), COALESCE(span_id,''), COALESCE(parent_span_id,''), COALESCE(service_name,'unknown'), COALESCE(span_name,''),
 COALESCE(json_extract(data,'$.kind'),''), COALESCE(service_version,''), COALESCE(start_time_unix_nano,0), COALESCE(end_time_unix_nano,0), COALESCE(status_code,0),
 COALESCE((SELECT substr(json_extract(ev.value,'$.attributes."exception.type"'),1,256) FROM json_each(spans.data,'$.events') ev WHERE json_extract(ev.value,'$.name')='exception' LIMIT 1),''),
 (SELECT json_group_object(a.key, CASE WHEN a.type IN ('integer','real') THEN a.value WHEN a.type='text' THEN substr(a.value,1,256) ELSE NULL END) FROM json_each(spans.data,'$.attributes') a WHERE a.key IN (` + strings.Join(keys, ",") + `))
 FROM spans WHERE ` + where + ` ORDER BY start_time_unix_nano, id LIMIT ?`
	args = append(args, InsightSpanLimit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Observation{}
	for rows.Next() {
		var o Observation
		var attrs string
		if err = rows.Scan(&o.TraceID, &o.SpanID, &o.ParentID, &o.Service, &o.Operation, &o.Kind, &o.Version, &o.Start, &o.End, &o.Status, &o.ErrorType, &attrs); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(attrs), &o.Attributes); err != nil {
			return nil, err
		}
		out = append(out, o)
		if len(out) > InsightSpanLimit {
			return nil, ErrInsightLimit
		}
	}
	return out, rows.Err()
}

// Explore returns COMPLETE trace summaries, selected by a matching span.
func (s *Store) Explore(ctx context.Context, q InsightQuery, limit int) ([]TraceSummary, error) {
	where, args := insightWhere(q, true)
	query := `WITH selected AS (SELECT DISTINCT trace_id FROM spans WHERE ` + where + `), roots AS (
 SELECT trace_id,start_time_unix_nano,end_time_unix_nano,status_code,
 FIRST_VALUE(service_name) OVER w root_service, FIRST_VALUE(span_name) OVER w root_name
 FROM spans WHERE trace_id IN (SELECT trace_id FROM selected)
 WINDOW w AS (PARTITION BY trace_id ORDER BY CASE WHEN parent_span_id IS NULL OR parent_span_id IN ('','0000000000000000') THEN 0 ELSE 1 END,start_time_unix_nano,id)
 ) SELECT trace_id,MIN(start_time_unix_nano),MAX(end_time_unix_nano),COUNT(*),COALESCE(MAX(status_code),0),COALESCE(MAX(root_service),''),COALESCE(MAX(root_name),'')
 FROM roots GROUP BY trace_id ORDER BY MIN(start_time_unix_nano) DESC,trace_id LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TraceSummary{}
	for rows.Next() {
		var t TraceSummary
		var end int64
		if err = rows.Scan(&t.TraceID, &t.StartTimeUnixNano, &end, &t.SpanCount, &t.StatusCode, &t.RootServiceName, &t.RootTraceName); err != nil {
			return nil, err
		}
		if end > t.StartTimeUnixNano {
			t.DurationMs = (end - t.StartTimeUnixNano) / 1e6
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

type InsightService struct {
	Service  string   `json:"service"`
	LastSeen float64  `json:"last_seen"`
	InWindow bool     `json:"in_window"`
	Versions []string `json:"versions"`
}

// Freshness deliberately includes services with no spans in the selected window.
// last_seen describes retained span event time, NOT uptime or ingest health.
func (s *Store) InsightServices(ctx context.Context, q InsightQuery) ([]InsightService, error) {
	where := "service_name IS NOT NULL"
	args := []interface{}{}
	if q.Service != "" {
		where += " AND service_name=?"
		args = append(args, q.Service)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT service_name,MAX(start_time_unix_nano),MAX(CASE WHEN start_time_unix_nano>=? AND start_time_unix_nano<? THEN 1 ELSE 0 END) FROM spans WHERE `+where+` GROUP BY service_name ORDER BY service_name LIMIT 1001`, append([]interface{}{q.From * 1e9, q.To * 1e9}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InsightService{}
	for rows.Next() {
		var x InsightService
		var ns int64
		var present int
		if err = rows.Scan(&x.Service, &ns, &present); err != nil {
			return nil, err
		}
		x.LastSeen = float64(ns) / 1e9
		x.InWindow = present == 1
		x.Versions = []string{}
		out = append(out, x)
		if len(out) > 1000 {
			return nil, ErrInsightLimit
		}
	}
	return out, rows.Err()
}

// ReadBundleTraces enforces bounds before reading payloads, within one snapshot.
func (s *Store) ReadBundleTraces(ctx context.Context, ids []string) (map[string][]json.RawMessage, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := make(map[string][]json.RawMessage)
	total, bytes := 0, 0
	for _, id := range ids {
		var count, size int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*),COALESCE(SUM(length(CAST(data AS BLOB))),0) FROM spans WHERE trace_id=?", id).Scan(&count, &size); err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, fmt.Errorf("trace not found: %s", id)
		}
		total += count
		bytes += size
		if total > InsightSpanLimit || bytes > BundleByteLimit {
			return nil, ErrInsightLimit
		}
		rows, err := tx.QueryContext(ctx, "SELECT data FROM spans WHERE trace_id=? ORDER BY start_time_unix_nano,id", id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var data string
			if err = rows.Scan(&data); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = append(out[id], json.RawMessage(data))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, tx.Commit()
}
