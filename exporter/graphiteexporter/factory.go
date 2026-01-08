package graphiteexporter

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
)

const (
	// Default values for configuration
	defaultEndpoint    = "localhost:2003"
	defaultTimeout     = 10 * time.Second
	defaultPrefix      = "otel"
	defaultSendMetrics = true
)

// TypeStr is the component.Type for this exporter
var TypeStr component.Type = "graphite"

// NewFactory creates a new factory for the Graphite exporter
func NewFactory() exporter.Factory {
	return exporter.NewFactory(
		TypeStr,
		createDefaultConfig,
		exporter.WithTraces(createTracesExporter, component.StabilityLevelDevelopment),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		Endpoint:    defaultEndpoint,
		Timeout:     defaultTimeout,
		Prefix:      defaultPrefix,
		SendMetrics: defaultSendMetrics,
	}
}

func createTracesExporter(
	ctx context.Context,
	set exporter.CreateSettings,
	cfg component.Config,
) (exporter.Traces, error) {
	expCfg := cfg.(*Config)

	exp, err := newGraphiteExporter(expCfg, set.Logger)
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
		exporterhelper.WithTimeout(exporterhelper.TimeoutSettings{Timeout: expCfg.Timeout}),
		exporterhelper.WithRetry(exporterhelper.RetrySettings{Enabled: true}),
		exporterhelper.WithQueue(exporterhelper.QueueSettings{Enabled: true}),
	)
}
