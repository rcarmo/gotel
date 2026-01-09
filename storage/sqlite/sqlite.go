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

// SpanRecord represents a stored span with extracted fields
type SpanRecord struct {
	ID            int64           `json:"id"`
	TraceID       string          `json:"trace_id"`
	SpanID        string          `json:"span_id"`
	ParentSpanID  string          `json:"parent_span_id,omitempty"`
	ServiceName   string          `json:"service_name"`
	SpanName      string          `json:"span_name"`
	StartTime     time.Time       `json:"start_time"`
	EndTime       time.Time       `json:"end_time"`
	DurationMs    int64           `json:"duration_ms"`
	StatusCode    int             `json:"status_code"`
	StatusMessage string          `json:"status_message,omitempty"`
	Data          json.RawMessage `json:"data"` // Full span JSON
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

	// Set connection pool settings for concurrent access
	db.SetMaxOpenConns(1) // SQLite works best with single writer
	db.SetMaxIdleConns(1)
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
		status_code INTEGER GENERATED ALWAYS AS (json_extract(data, '$.status.code')) VIRTUAL
	);

	-- Indexes for common query patterns
	CREATE INDEX IF NOT EXISTS idx_spans_trace_id ON spans(trace_id);
	CREATE INDEX IF NOT EXISTS idx_spans_service_name ON spans(service_name);
	CREATE INDEX IF NOT EXISTS idx_spans_span_name ON spans(span_name);
	CREATE INDEX IF NOT EXISTS idx_spans_start_time ON spans(start_time_unix_nano);
	CREATE INDEX IF NOT EXISTS idx_spans_status_code ON spans(status_code);
	CREATE INDEX IF NOT EXISTS idx_spans_service_span ON spans(service_name, span_name);
	CREATE INDEX IF NOT EXISTS idx_spans_created_at ON spans(created_at);
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

	// Trace aggregates for faster trace listing
	aggregatesSchema := `
	CREATE TABLE IF NOT EXISTS trace_summaries (
		trace_id TEXT PRIMARY KEY,
		root_service TEXT,
		root_span TEXT,
		span_count INTEGER DEFAULT 0,
		error_count INTEGER DEFAULT 0,
		start_time INTEGER,
		end_time INTEGER,
		duration_ns INTEGER,
		updated_at INTEGER DEFAULT (strftime('%s', 'now'))
	);

	CREATE INDEX IF NOT EXISTS idx_trace_summaries_root_service ON trace_summaries(root_service);
	CREATE INDEX IF NOT EXISTS idx_trace_summaries_start_time ON trace_summaries(start_time);
	`

	for _, schema := range []string{spansSchema, metricsSchema, aggregatesSchema} {
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

// InsertSpanBatch stores multiple spans in a single transaction
func (s *Store) InsertSpanBatch(ctx context.Context, spans [][]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

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

	return tx.Commit()
}

// InsertMetric stores a metric data point
func (s *Store) InsertMetric(ctx context.Context, name string, value float64, timestamp int64, tags map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		"INSERT INTO metrics (name, value, timestamp, tags) VALUES (?, ?, ?, ?)",
		name, value, timestamp, string(tagsJSON))
	return err
}

// InsertMetricBatch stores multiple metrics in a single transaction
func (s *Store) InsertMetricBatch(ctx context.Context, metrics []MetricRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

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
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
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

// QueryMetrics retrieves metrics matching the given pattern
func (s *Store) QueryMetrics(ctx context.Context, opts MetricQueryOptions) ([]MetricRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT id, name, value, timestamp, tags FROM metrics WHERE 1=1"
	args := []interface{}{}

	if opts.Name != "" {
		if opts.NamePattern {
			query += " AND name LIKE ?"
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
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
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

	// Delete old trace summaries
	s.db.ExecContext(ctx, "DELETE FROM trace_summaries WHERE updated_at < ?", cutoff)

	return spansDeleted + metricsDeleted, nil
}

// Stats returns storage statistics
func (s *Store) Stats(ctx context.Context) (StorageStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var stats StorageStats

	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM spans").Scan(&stats.SpanCount)
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM metrics").Scan(&stats.MetricCount)
	s.db.QueryRowContext(ctx, "SELECT COUNT(DISTINCT trace_id) FROM spans").Scan(&stats.TraceCount)
	s.db.QueryRowContext(ctx, "SELECT COUNT(DISTINCT service_name) FROM spans").Scan(&stats.ServiceCount)

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
