package graphiteexporter

import (
	"errors"
	"time"
)

// Config defines the configuration for the Graphite exporter
type Config struct {
	// Endpoint is the Graphite server address (host:port)
	// Default: localhost:2003
	Endpoint string `mapstructure:"endpoint"`

	// Timeout is the connection and write timeout
	// Default: 10s
	Timeout time.Duration `mapstructure:"timeout"`

	// Prefix is the metric prefix to use for all metrics
	// Default: otel
	Prefix string `mapstructure:"prefix"`

	// SendMetrics enables sending derived metrics from traces
	// (span counts, duration histograms, error rates)
	// Default: true
	SendMetrics bool `mapstructure:"send_metrics"`

	// TagSupport enables Graphite tag support (for Graphite 1.1+)
	// When enabled, uses tagged metric format: metric;tag1=value1;tag2=value2
	// Default: false
	TagSupport bool `mapstructure:"tag_support"`

	// Namespace adds an additional namespace prefix
	// Format: prefix.namespace.metric
	Namespace string `mapstructure:"namespace"`
}

// Validate checks the configuration for errors
func (cfg *Config) Validate() error {
	if cfg.Endpoint == "" {
		return errors.New("endpoint cannot be empty")
	}
	if cfg.Timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	return nil
}
