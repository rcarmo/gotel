package sqliteexporter

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config defines the configuration for the SQLite exporter
type Config struct {
	// DBPath is the path to the SQLite database file
	// Default: gotel.db
	DBPath string `mapstructure:"db_path"`

	// Prefix is the metric prefix to use for all metrics
	// Default: otel
	Prefix string `mapstructure:"prefix"`

	// Namespace adds an additional namespace prefix
	// Format: prefix.namespace.metric
	Namespace string `mapstructure:"namespace"`

	// SendMetrics enables sending derived metrics from traces
	// (span counts, duration histograms, error rates)
	// Default: true
	SendMetrics bool `mapstructure:"send_metrics"`

	// StoreTraces enables storing raw trace data
	// Default: true
	StoreTraces bool `mapstructure:"store_traces"`

	// TagSupport enables tag support in metric names
	// Default: false
	TagSupport bool `mapstructure:"tag_support"`

	// Retention is the duration to keep data before cleanup
	// Default: 168h (7 days)
	Retention time.Duration `mapstructure:"retention"`

	// CleanupInterval is how often to run cleanup
	// Default: 1h
	CleanupInterval time.Duration `mapstructure:"cleanup_interval"`

	// QueryPort is the HTTP port for the query API (0 to disable)
	// Default: 3200
	QueryPort int `mapstructure:"query_port"`
}

// applyEnvironmentOverrides reads well-known environment variables and applies
// them to the config. This is separated from Validate so that overrides are
// applied exactly once during construction, not on every validation call.
func (cfg *Config) applyEnvironmentOverrides() error {
	if envDBPath := strings.TrimSpace(os.Getenv("GOTEL_DB_PATH")); envDBPath != "" {
		cfg.DBPath = envDBPath
	}
	if envRetention := strings.TrimSpace(os.Getenv("GOTEL_RETENTION")); envRetention != "" {
		d, err := time.ParseDuration(envRetention)
		if err != nil {
			return fmt.Errorf("invalid GOTEL_RETENTION %q: %w", envRetention, err)
		}
		cfg.Retention = d
	}
	return nil
}

// Validate checks the configuration for errors and applies defaults.
func (cfg *Config) Validate() error {
	if cfg.DBPath == "" {
		cfg.DBPath = "gotel.db"
	}
	if cfg.Prefix == "" {
		cfg.Prefix = "otel"
	}
	if cfg.Retention == 0 {
		cfg.Retention = defaultRetention
	}
	if cfg.CleanupInterval == 0 {
		cfg.CleanupInterval = time.Hour
	}
	return nil
}
