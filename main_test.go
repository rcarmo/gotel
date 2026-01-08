package main

import (
	"testing"
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

	// Verify exporters
	if len(factories.Exporters) != 1 {
		t.Errorf("Expected 1 exporter, got %d", len(factories.Exporters))
	}
}
