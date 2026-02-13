// Package sqlite provides a SQLite-based storage backend for traces and metrics
// using WAL mode and JSON virtual columns with indexes for efficient querying.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Store is a SQLite-backed storage for traces and metrics
type Store struct {
	db     *sql.DB
	dbPath string
	mu     sync.RWMutex
}

// MetricRecord represents a stored metric data point
type MetricRecord struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Value     float64 `json:"value"`
	Timestamp int64   `json:"timestamp"`
	Tags      string  `json:"tags"` // JSON object of tags
}

// New creates a new SQLite store at the given path
func New(dbPath string) (*Store, error) {
	// Use WAL mode and other optimizations via connection string
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000&_cache_size=-64000", dbPath)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// SQLite WAL mode supports concurrent readers with a single writer.
	// Allow multiple read connections but limit writes via application-level mutex.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0)

	store := &Store{
		db:     db,
		dbPath: dbPath,
	}

	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

// initSchema creates tables with JSON columns, virtual columns, and indexes
func (s *Store) initSchema() error {
	// Spans table: raw JSON with virtual indexed columns
	spansSchema := `
	CREATE TABLE IF NOT EXISTS spans (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		data TEXT NOT NULL,
		created_at INTEGER DEFAULT (strftime('%s', 'now')),
		
		-- Virtual generated columns extracted from JSON for indexing
		trace_id TEXT GENERATED ALWAYS AS (json_extract(data, '$.trace_id')) VIRTUAL,
		span_id TEXT GENERATED ALWAYS AS (json_extract(data, '$.span_id')) VIRTUAL,
		parent_span_id TEXT GENERATED ALWAYS AS (json_extract(data, '$.parent_span_id')) VIRTUAL,
		service_name TEXT GENERATED ALWAYS AS (json_extract(data, '$.service_name')) VIRTUAL,
		span_name TEXT GENERATED ALWAYS AS (json_extract(data, '$.span_name')) VIRTUAL,
		start_time_unix_nano INTEGER GENERATED ALWAYS AS (json_extract(data, '$.start_time_unix_nano')) VIRTUAL,
		end_time_unix_nano INTEGER GENERATED ALWAYS AS (json_extract(data, '$.end_time_unix_nano')) VIRTUAL,
		duration_ns INTEGER GENERATED ALWAYS AS (json_extract(data, '$.end_time_unix_nano') - json_extract(data, '$.start_time_unix_nano')) VIRTUAL,
		status_code INTEGER GENERATED ALWAYS AS (json_extract(data, '$.status.code')) VIRTUAL,
		
		-- Resource attribute virtual columns for common queries
		service_version TEXT GENERATED ALWAYS AS (json_extract(data, '$.resource."service.version"')) VIRTUAL,
		deployment_environment TEXT GENERATED ALWAYS AS (json_extract(data, '$.resource."deployment.environment"')) VIRTUAL,
		
		-- Instrumentation scope
		scope_name TEXT GENERATED ALWAYS AS (json_extract(data, '$.scope.name')) VIRTUAL
	);

	-- Indexes for common query patterns
	CREATE INDEX IF NOT EXISTS idx_spans_trace_id ON spans(trace_id);
	CREATE INDEX IF NOT EXISTS idx_spans_service_name ON spans(service_name);
	CREATE INDEX IF NOT EXISTS idx_spans_span_name ON spans(span_name);
	CREATE INDEX IF NOT EXISTS idx_spans_start_time ON spans(start_time_unix_nano);
	CREATE INDEX IF NOT EXISTS idx_spans_status_code ON spans(status_code);
	CREATE INDEX IF NOT EXISTS idx_spans_service_span ON spans(service_name, span_name);
	CREATE INDEX IF NOT EXISTS idx_spans_created_at ON spans(created_at);
	CREATE INDEX IF NOT EXISTS idx_spans_service_version ON spans(service_version);
	CREATE INDEX IF NOT EXISTS idx_spans_deployment_env ON spans(deployment_environment);
	CREATE INDEX IF NOT EXISTS idx_spans_scope_name ON spans(scope_name);
	`

	// Metrics table: time-series data with tags
	metricsSchema := `
	CREATE TABLE IF NOT EXISTS metrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		value REAL NOT NULL,
		timestamp INTEGER NOT NULL,
		tags TEXT DEFAULT '{}',
		
		-- Virtual columns for common tag extractions
		service TEXT GENERATED ALWAYS AS (json_extract(tags, '$.service')) VIRTUAL,
		span TEXT GENERATED ALWAYS AS (json_extract(tags, '$.span')) VIRTUAL
	);

	-- Indexes for metric queries
	CREATE INDEX IF NOT EXISTS idx_metrics_name ON metrics(name);
	CREATE INDEX IF NOT EXISTS idx_metrics_timestamp ON metrics(timestamp);
	CREATE INDEX IF NOT EXISTS idx_metrics_name_timestamp ON metrics(name, timestamp);
	CREATE INDEX IF NOT EXISTS idx_metrics_service ON metrics(service);
	`

	for _, schema := range []string{spansSchema, metricsSchema} {
		if _, err := s.db.Exec(schema); err != nil {
			return fmt.Errorf("failed to execute schema: %w", err)
		}
	}

	return nil
}

// InsertSpan stores a span as raw JSON
func (s *Store) InsertSpan(ctx context.Context, spanJSON []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, "INSERT INTO spans (data) VALUES (?)", string(spanJSON))
	return err
}

// InsertMetric stores a metric data point
func (s *Store) InsertMetric(ctx context.Context, name string, value float64, timestamp int64, tags map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if tags == nil {
		tags = map[string]string{}
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		"INSERT INTO metrics (name, value, timestamp, tags) VALUES (?, ?, ?, ?)",
		name, value, timestamp, string(tagsJSON))
	return err
}

// InsertData stores spans and metrics in a single transaction for atomicity
func (s *Store) InsertData(ctx context.Context, spans [][]byte, metrics []MetricRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if len(spans) > 0 {
		stmt, err := tx.PrepareContext(ctx, "INSERT INTO spans (data) VALUES (?)")
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, spanJSON := range spans {
			if _, err := stmt.ExecContext(ctx, string(spanJSON)); err != nil {
				return err
			}
		}
	}

	if len(metrics) > 0 {
		stmt, err := tx.PrepareContext(ctx, "INSERT INTO metrics (name, value, timestamp, tags) VALUES (?, ?, ?, ?)")
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, m := range metrics {
			if _, err := stmt.ExecContext(ctx, m.Name, m.Value, m.Timestamp, m.Tags); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// QueryTraceByID retrieves all spans for a given trace ID
func (s *Store) QueryTraceByID(ctx context.Context, traceID string) ([]json.RawMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx,
		"SELECT data FROM spans WHERE trace_id = ? ORDER BY start_time_unix_nano",
		traceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var spans []json.RawMessage
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		spans = append(spans, json.RawMessage(data))
	}
	return spans, rows.Err()
}

// QuerySpans searches spans with filters
func (s *Store) QuerySpans(ctx context.Context, opts SpanQueryOptions) ([]json.RawMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT data FROM spans WHERE 1=1"
	args := []interface{}{}

	if opts.ServiceName != "" {
		query += " AND service_name = ?"
		args = append(args, opts.ServiceName)
	}
	if opts.SpanName != "" {
		query += " AND span_name = ?"
		args = append(args, opts.SpanName)
	}
	if opts.MinStartTime > 0 {
		query += " AND start_time_unix_nano >= ?"
		args = append(args, opts.MinStartTime)
	}
	if opts.MaxStartTime > 0 {
		query += " AND start_time_unix_nano <= ?"
		args = append(args, opts.MaxStartTime)
	}
	if opts.StatusCode != nil {
		query += " AND status_code = ?"
		args = append(args, *opts.StatusCode)
	}

	query += " ORDER BY start_time_unix_nano DESC"

	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var spans []json.RawMessage
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		spans = append(spans, json.RawMessage(data))
	}
	return spans, rows.Err()
}

// QuerySpansByTime retrieves spans within a time range with advanced filtering
func (s *Store) QuerySpansByTime(ctx context.Context, opts SpanTimeQueryOptions) ([]json.RawMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT data FROM spans WHERE 1=1"
	args := []interface{}{}

	if opts.ServiceName != "" {
		query += " AND service_name = ?"
		args = append(args, opts.ServiceName)
	}
	if opts.SpanName != "" {
		query += " AND span_name = ?"
		args = append(args, opts.SpanName)
	}
	if opts.MinStartTime > 0 {
		query += " AND start_time_unix_nano >= ?"
		args = append(args, opts.MinStartTime)
	}
	if opts.MaxStartTime > 0 {
		query += " AND start_time_unix_nano <= ?"
		args = append(args, opts.MaxStartTime)
	}
	if opts.MinEndTime > 0 {
		query += " AND end_time_unix_nano >= ?"
		args = append(args, opts.MinEndTime)
	}
	if opts.MaxEndTime > 0 {
		query += " AND end_time_unix_nano <= ?"
		args = append(args, opts.MaxEndTime)
	}
	if opts.StatusCode != nil {
		query += " AND status_code = ?"
		args = append(args, *opts.StatusCode)
	}
	if opts.MinDuration != nil {
		query += " AND (end_time_unix_nano - start_time_unix_nano) >= ?"
		args = append(args, *opts.MinDuration*int64(time.Millisecond))
	}
	if opts.MaxDuration != nil {
		query += " AND (end_time_unix_nano - start_time_unix_nano) <= ?"
		args = append(args, *opts.MaxDuration*int64(time.Millisecond))
	}

	query += " ORDER BY start_time_unix_nano DESC"

	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}
	if opts.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, opts.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var spans []json.RawMessage
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		spans = append(spans, json.RawMessage(data))
	}
	return spans, rows.Err()
}

// SpanQueryOptions defines filters for span queries
type SpanQueryOptions struct {
	ServiceName  string
	SpanName     string
	MinStartTime int64
	MaxStartTime int64
	StatusCode   *int
	Limit        int
}

// SpanTimeQueryOptions defines filters for time-based span queries
type SpanTimeQueryOptions struct {
	ServiceName  string
	SpanName     string
	MinStartTime int64
	MaxStartTime int64
	MinEndTime   int64
	MaxEndTime   int64
	StatusCode   *int
	MinDuration  *int64 // milliseconds
	MaxDuration  *int64 // milliseconds
	Limit        int
	Offset       int
}

// TraceSearchOptions defines filters for trace search.
//
// This is intentionally small: it supports the subset of Tempo search parameters
// that Grafana commonly uses.
type TraceSearchOptions struct {
	ServiceName  string
	SpanName     string
	MinStartTime int64
	MaxStartTime int64
	Limit        int
}

// TraceSummary is a lightweight description of a trace, suitable for search results.
type TraceSummary struct {
	TraceID           string
	RootServiceName   string
	RootTraceName     string
	StartTimeUnixNano int64
	DurationMs        int64
	SpanCount         int64
	StatusCode        int
}

// SearchTraces returns trace summaries, grouped by trace_id.
func (s *Store) SearchTraces(ctx context.Context, opts TraceSearchOptions) ([]TraceSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		WITH filtered AS (
			SELECT
				trace_id,
				service_name,
				span_name,
				parent_span_id,
				start_time_unix_nano,
				end_time_unix_nano,
				status_code
			FROM spans
			WHERE trace_id IS NOT NULL
	`

	args := []interface{}{}
	if opts.ServiceName != "" {
		query += " AND trace_id IN (SELECT trace_id FROM spans WHERE service_name = ?)"
		args = append(args, opts.ServiceName)
	}
	if opts.SpanName != "" {
		query += " AND trace_id IN (SELECT trace_id FROM spans WHERE span_name = ?)"
		args = append(args, opts.SpanName)
	}
	if opts.MinStartTime > 0 && opts.MaxStartTime > 0 {
		query += " AND trace_id IN (SELECT trace_id FROM spans WHERE start_time_unix_nano >= ? AND start_time_unix_nano <= ?)"
		args = append(args, opts.MinStartTime, opts.MaxStartTime)
	} else {
		if opts.MinStartTime > 0 {
			query += " AND trace_id IN (SELECT trace_id FROM spans WHERE start_time_unix_nano >= ?)"
			args = append(args, opts.MinStartTime)
		}
		if opts.MaxStartTime > 0 {
			query += " AND trace_id IN (SELECT trace_id FROM spans WHERE start_time_unix_nano <= ?)"
			args = append(args, opts.MaxStartTime)
		}
	}

	query += `
		)
		, roots AS (
			SELECT
				trace_id,
				FIRST_VALUE(service_name) OVER w AS root_service,
				FIRST_VALUE(span_name) OVER w AS root_name,
				start_time_unix_nano,
				end_time_unix_nano,
				status_code
			FROM filtered
			WINDOW w AS (
				PARTITION BY trace_id
				ORDER BY
					CASE
						WHEN parent_span_id IS NULL OR parent_span_id = '' OR parent_span_id = '0000000000000000' THEN 0
						ELSE 1
					END,
					start_time_unix_nano
			)
		)
		SELECT
			trace_id,
			MIN(start_time_unix_nano) AS start_ns,
			MAX(end_time_unix_nano) AS end_ns,
			COUNT(*) AS span_count,
			MAX(status_code) AS max_status,
			MAX(root_service) AS root_service,
			MAX(root_name) AS root_name
		FROM roots
		WHERE trace_id IS NOT NULL
	`

	query += " GROUP BY trace_id ORDER BY start_ns DESC"
	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TraceSummary
	for rows.Next() {
		var traceID string
		var startNs, endNs, spanCount int64
		var maxStatus int
		var rootService, rootName sql.NullString
		if err := rows.Scan(&traceID, &startNs, &endNs, &spanCount, &maxStatus, &rootService, &rootName); err != nil {
			return nil, err
		}

		durationMs := int64(0)
		if endNs > startNs {
			durationMs = (endNs - startNs) / int64(time.Millisecond)
		}

		out = append(out, TraceSummary{
			TraceID:           traceID,
			RootServiceName:   rootService.String,
			RootTraceName:     rootName.String,
			StartTimeUnixNano: startNs,
			DurationMs:        durationMs,
			SpanCount:         spanCount,
			StatusCode:        maxStatus,
		})
	}
	return out, rows.Err()
}

// QueryMetrics retrieves metrics matching the given pattern
func (s *Store) QueryMetrics(ctx context.Context, opts MetricQueryOptions) ([]MetricRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT id, name, value, timestamp, tags FROM metrics WHERE 1=1"
	args := []interface{}{}

	if opts.Name != "" {
		if opts.NamePattern {
			query += " AND name LIKE ? ESCAPE '\\'"
			args = append(args, opts.Name)
		} else {
			query += " AND name = ?"
			args = append(args, opts.Name)
		}
	}
	if opts.MinTime > 0 {
		query += " AND timestamp >= ?"
		args = append(args, opts.MinTime)
	}
	if opts.MaxTime > 0 {
		query += " AND timestamp <= ?"
		args = append(args, opts.MaxTime)
	}

	query += " ORDER BY timestamp"

	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []MetricRecord
	for rows.Next() {
		var m MetricRecord
		if err := rows.Scan(&m.ID, &m.Name, &m.Value, &m.Timestamp, &m.Tags); err != nil {
			return nil, err
		}
		metrics = append(metrics, m)
	}
	return metrics, rows.Err()
}

// MetricQueryOptions defines filters for metric queries
type MetricQueryOptions struct {
	Name        string
	NamePattern bool // If true, use LIKE pattern matching
	MinTime     int64
	MaxTime     int64
	Limit       int
}

// ListServices returns unique service names
func (s *Store) ListServices(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx,
		"SELECT DISTINCT service_name FROM spans WHERE service_name IS NOT NULL ORDER BY service_name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var services []string
	for rows.Next() {
		var svc string
		if err := rows.Scan(&svc); err != nil {
			return nil, err
		}
		services = append(services, svc)
	}
	return services, rows.Err()
}

// ListOperations returns unique span names for a service
func (s *Store) ListOperations(ctx context.Context, serviceName string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx,
		"SELECT DISTINCT span_name FROM spans WHERE service_name = ? ORDER BY span_name",
		serviceName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ops []string
	for rows.Next() {
		var op string
		if err := rows.Scan(&op); err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	return ops, rows.Err()
}

// Cleanup removes data older than the given duration
func (s *Store) Cleanup(ctx context.Context, retention time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-retention).Unix()

	// Delete old spans
	result, err := s.db.ExecContext(ctx, "DELETE FROM spans WHERE created_at < ?", cutoff)
	if err != nil {
		return 0, err
	}
	spansDeleted, _ := result.RowsAffected()

	// Delete old metrics
	result, err = s.db.ExecContext(ctx, "DELETE FROM metrics WHERE timestamp < ?", cutoff)
	if err != nil {
		return spansDeleted, err
	}
	metricsDeleted, _ := result.RowsAffected()

	return spansDeleted + metricsDeleted, nil
}

// Stats returns storage statistics
func (s *Store) Stats(ctx context.Context) (StorageStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var stats StorageStats

	// Single query for all span-related stats
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT trace_id), COUNT(DISTINCT service_name)
		FROM spans
	`).Scan(&stats.SpanCount, &stats.TraceCount, &stats.ServiceCount)
	if err != nil {
		return stats, fmt.Errorf("failed to query span stats: %w", err)
	}

	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM metrics").Scan(&stats.MetricCount); err != nil {
		return stats, fmt.Errorf("failed to count metrics: %w", err)
	}

	return stats, nil
}

// StorageStats contains storage statistics
type StorageStats struct {
	SpanCount    int64 `json:"span_count"`
	MetricCount  int64 `json:"metric_count"`
	TraceCount   int64 `json:"trace_count"`
	ServiceCount int64 `json:"service_count"`
}

// Close closes the database connection
func (s *Store) Close() error {
	return s.db.Close()
}

// Checkpoint forces a WAL checkpoint
func (s *Store) Checkpoint(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}
