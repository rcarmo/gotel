package main

import (
	"log"
	"os"
	"strings"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/otlpexporter"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/batchprocessor"
	"go.opentelemetry.io/collector/processor/memorylimiterprocessor"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"

	"github.com/gotel/exporter/graphiteexporter"
)

// Version and BuildTime are injected via -ldflags
var (
	Version   = "dev"
	BuildTime = ""
)

const defaultConfigYAML = `
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

processors:
  batch:
    timeout: 5s
    send_batch_size: 1000
  memory_limiter:
    check_interval: 1s
    limit_mib: 512
    spike_limit_mib: 128

exporters:
  graphite:
    endpoint: localhost:2003
    timeout: 10s
    prefix: otel
    namespace: traces
    send_metrics: true
    tag_support: false

  otlp/tempo:
    endpoint: ${TEMPO_ENDPOINT:-tempo:4317}
    tls:
      insecure: true

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [graphite, otlp/tempo]
`

func main() {
	info := component.BuildInfo{
		Command:     "gotel",
		Description: "OpenTelemetry Collector with Graphite Exporter",
		Version:     Version,
	}

	params := otelcol.CollectorSettings{
		BuildInfo: info,
		Factories: components,
	}

	args := os.Args[1:]
	var tmpConfigPath string
	if !hasConfigArg(args) {
		configFile := os.Getenv("OTEL_CONFIG_FILE")
		if configFile == "" {
			configFile = "config.yaml"
		}

		if _, err := os.Stat(configFile); os.IsNotExist(err) {
			tmp, err := os.CreateTemp("", "gotel-default-*.yaml")
			if err == nil {
				if _, writeErr := tmp.WriteString(strings.ReplaceAll(defaultConfigYAML, "\t", "  ")); writeErr == nil {
					tmp.Close()
					tmpConfigPath = tmp.Name()
					args = append([]string{"--config", tmpConfigPath}, args...)
				} else {
					tmp.Close()
					os.Remove(tmp.Name())
				}
			}
		}
	}
	if tmpConfigPath != "" {
		defer os.Remove(tmpConfigPath)
	}

	cmd := otelcol.NewCommand(params)
	if len(args) > 0 {
		cmd.SetArgs(args)
	}

	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func hasConfigArg(args []string) bool {
	for _, a := range args {
		if a == "--config" || a == "-c" {
			return true
		}
		if strings.HasPrefix(a, "--config=") {
			return true
		}
	}
	return false
}

func components() (otelcol.Factories, error) {
	otlpReceiverFactory := otlpreceiver.NewFactory()
	otlpExporterFactory := otlpexporter.NewFactory()
	batchProcessorFactory := batchprocessor.NewFactory()
	memoryLimiterFactory := memorylimiterprocessor.NewFactory()
	graphiteFactory := graphiteexporter.NewFactory()

	factories := otelcol.Factories{
		Receivers: map[component.Type]receiver.Factory{
			otlpReceiverFactory.Type(): otlpReceiverFactory,
		},
		Processors: map[component.Type]processor.Factory{
			batchProcessorFactory.Type(): batchProcessorFactory,
			memoryLimiterFactory.Type():  memoryLimiterFactory,
		},
		Exporters: map[component.Type]exporter.Factory{
			graphiteFactory.Type():     graphiteFactory,
			otlpExporterFactory.Type(): otlpExporterFactory,
		},
	}
	return factories, nil
}
