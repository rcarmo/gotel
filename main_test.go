package main

import (
	"strings"
	"testing"

	"github.com/gotel/exporter/sqliteexporter"
)

func TestHasConfigArg(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "no args",
			args:     []string{},
			expected: false,
		},
		{
			name:     "with --config",
			args:     []string{"--config", "config.yaml"},
			expected: true,
		},
		{
			name:     "with -c",
			args:     []string{"-c", "config.yaml"},
			expected: true,
		},
		{
			name:     "with --config=value",
			args:     []string{"--config=config.yaml"},
			expected: true,
		},
		{
			name:     "other args only",
			args:     []string{"--help", "--version"},
			expected: false,
		},
		{
			name:     "config in middle",
			args:     []string{"--verbose", "--config", "config.yaml", "--debug"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasConfigArg(tt.args)
			if result != tt.expected {
				t.Errorf("hasConfigArg(%v) = %v, want %v", tt.args, result, tt.expected)
			}
		})
	}
}

func TestComponents(t *testing.T) {
	factories, err := components()
	if err != nil {
		t.Fatalf("components() error = %v", err)
	}

	// Verify receivers
	if len(factories.Receivers) != 1 {
		t.Errorf("Expected 1 receiver, got %d", len(factories.Receivers))
	}

	// Verify processors
	if len(factories.Processors) != 2 {
		t.Errorf("Expected 2 processors, got %d", len(factories.Processors))
	}

	// Verify SQLite exporter is registered
	if len(factories.Exporters) != 1 {
		t.Errorf("Expected 1 exporter, got %d", len(factories.Exporters))
	}

	if _, ok := factories.Exporters[sqliteexporter.TypeStr]; !ok {
		t.Errorf("sqlite exporter not registered")
	}
}

func TestDefaultConfigYAMLIncludesSQLiteExporter(t *testing.T) {
	if !strings.Contains(defaultConfigYAML, "sqlite:") {
		t.Fatalf("defaultConfigYAML missing sqlite block")
	}
	if !strings.Contains(defaultConfigYAML, "${GOTEL_DB_PATH:-gotel.db}") {
		t.Fatalf("defaultConfigYAML missing GOTEL_DB_PATH override")
	}
	if !strings.Contains(defaultConfigYAML, "store_traces: true") {
		t.Fatalf("defaultConfigYAML missing store_traces option")
	}
	if !strings.Contains(defaultConfigYAML, "exporters: [sqlite]") {
		t.Fatalf("defaultConfigYAML missing sqlite in exporters list")
	}
}

