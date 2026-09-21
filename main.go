package main

import (
	"log"
	"os"
	"strings"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/provider/envprovider"
	"go.opentelemetry.io/collector/confmap/provider/fileprovider"
	"go.opentelemetry.io/collector/confmap/provider/yamlprovider"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/batchprocessor"
	"go.opentelemetry.io/collector/processor/memorylimiterprocessor"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"go.opentelemetry.io/collector/service/telemetry/otelconftelemetry"

	"github.com/gotel/exporter/sqliteexporter"
)

// Version and BuildTime are injected via -ldflags
var (
	Version   = "dev"
	BuildTime = ""
)

const defaultConfigYAML = "" +
	"receivers:\n" +
	"  otlp:\n" +
	"    protocols:\n" +
	"      grpc:\n" +
	"        endpoint: ${env:GOTEL_OTLP_HOST:-127.0.0.1}:4317\n" +
	"      http:\n" +
	"        endpoint: ${env:GOTEL_OTLP_HOST:-127.0.0.1}:4318\n" +
	"\n" +
	"processors:\n" +
	"  batch:\n" +
	"    timeout: 5s\n" +
	"    send_batch_size: 1000\n" +
	"  memory_limiter:\n" +
	"    check_interval: 1s\n" +
	"    limit_mib: 512\n" +
	"    spike_limit_mib: 128\n" +
	"\n" +
	"exporters:\n" +
	"  sqlite:\n" +
	"    db_path: gotel.db\n" +
	"    prefix: otel\n" +
	"    namespace: \"\"\n" +
	"    send_metrics: true\n" +
	"    store_traces: true\n" +
	"    retention: 168h\n" +
	"    cleanup_interval: 1h\n" +
	"    query_port: 3200\n" +
	"\n" +
	"service:\n" +
	"  pipelines:\n" +
	"    traces:\n" +
	"      receivers: [otlp]\n" +
	"      processors: [memory_limiter, batch]\n" +
	"      exporters: [sqlite]\n"

func main() {
	info := component.BuildInfo{
		Command:     "gotel",
		Description: "Self-contained OpenTelemetry Collector with SQLite storage",
		Version:     Version,
	}

	params := collectorSettings(info)

	args := os.Args[1:]
	if !hasConfigArg(args) {
		configFile := os.Getenv("GOTEL_CONFIG")
		if configFile == "" {
			configFile = os.Getenv("OTEL_CONFIG_FILE")
		}
		if configFile != "" {
			// Explicit configuration must fail closed if missing or unreadable.
			args = append([]string{"--config", configFile}, args...)
		} else if _, err := os.Stat("config.yaml"); os.IsNotExist(err) {
			args = append([]string{"--config", "yaml:" + defaultConfigYAML}, args...)
		} else {
			args = append([]string{"--config", "config.yaml"}, args...)
		}
	}

	cmd := otelcol.NewCommand(params)
	if len(args) > 0 {
		cmd.SetArgs(args)
	}

	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func collectorSettings(info component.BuildInfo) otelcol.CollectorSettings {
	return otelcol.CollectorSettings{
		BuildInfo: info,
		Factories: components,
		ConfigProviderSettings: otelcol.ConfigProviderSettings{
			ResolverSettings: confmap.ResolverSettings{
				ProviderFactories: []confmap.ProviderFactory{
					fileprovider.NewFactory(), yamlprovider.NewFactory(), envprovider.NewFactory(),
				},
				DefaultScheme: "env",
			},
		},
	}
}

func hasConfigArg(args []string) bool {
	for _, a := range args {
		if a == "--config" || a == "-c" {
			return true
		}
		if strings.HasPrefix(a, "--config=") || strings.HasPrefix(a, "-c=") {
			return true
		}
	}
	return false
}

func components() (otelcol.Factories, error) {
	otlpReceiverFactory := otlpreceiver.NewFactory()
	batchProcessorFactory := batchprocessor.NewFactory()
	memoryLimiterFactory := memorylimiterprocessor.NewFactory()
	sqliteFactory := sqliteexporter.NewFactory()

	factories := otelcol.Factories{
		Telemetry: otelconftelemetry.NewFactory(),
		Receivers: map[component.Type]receiver.Factory{
			otlpReceiverFactory.Type(): otlpReceiverFactory,
		},
		Processors: map[component.Type]processor.Factory{
			batchProcessorFactory.Type(): batchProcessorFactory,
			memoryLimiterFactory.Type():  memoryLimiterFactory,
		},
		Exporters: map[component.Type]exporter.Factory{
			sqliteFactory.Type(): sqliteFactory,
		},
	}
	return factories, nil
}
