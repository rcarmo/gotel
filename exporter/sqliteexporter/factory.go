package sqliteexporter

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
)

const (
	defaultDBPath          = "gotel.db"
	defaultPrefix          = "otel"
	defaultRetention       = 7 * 24 * time.Hour
	defaultCleanupInterval = time.Hour
	defaultQueryPort       = 3200
)

// TypeStr is the component.Type for this exporter
var TypeStr component.Type = "sqlite"

// NewFactory creates a new factory for the SQLite exporter
func NewFactory() exporter.Factory {
	return exporter.NewFactory(
		TypeStr,
		createDefaultConfig,
		exporter.WithTraces(createTracesExporter, component.StabilityLevelDevelopment),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		DBPath:          defaultDBPath,
		Prefix:          defaultPrefix,
		SendMetrics:     true,
		StoreTraces:     true,
		Retention:       defaultRetention,
		CleanupInterval: defaultCleanupInterval,
		QueryPort:       defaultQueryPort,
	}
}

func createTracesExporter(
	ctx context.Context,
	set exporter.CreateSettings,
	cfg component.Config,
) (exporter.Traces, error) {
	expCfg := cfg.(*Config)

	exp, err := newSQLiteExporter(expCfg, set.Logger)
	if err != nil {
		return nil, err
	}

	return exporterhelper.NewTracesExporter(
		ctx,
		set,
		cfg,
		exp.pushTraces,
		exporterhelper.WithStart(exp.start),
		exporterhelper.WithShutdown(exp.shutdown),
		exporterhelper.WithRetry(exporterhelper.RetrySettings{Enabled: false}),
		exporterhelper.WithQueue(exporterhelper.QueueSettings{Enabled: true, NumConsumers: 1}),
	)
}
