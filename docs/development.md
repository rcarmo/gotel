# Development Guide

## Project Structure

```
gotel/
├── main.go                              # Collector entry point
├── config.yaml                          # Default configuration
├── go.mod                               # Go module definition
├── go.sum                               # Dependency checksums
├── Makefile                             # Build automation
├── Dockerfile                           # Container image
├── docker-compose.yaml                  # Full stack deployment
├── README.md                            # Quick start guide
├── docs/                                # Documentation
│   ├── configuration.md                 # Configuration reference
│   ├── grafana.md                       # Grafana integration
│   ├── sending-traces.md                # Client examples
│   ├── development.md                   # This file
│   └── troubleshooting.md               # Common issues
├── exporter/
│   └── graphiteexporter/
│       ├── factory.go                   # Exporter factory
│       ├── config.go                    # Configuration struct
│       ├── exporter.go                  # Core export logic
│       └── exporter_test.go             # Unit tests
└── grafana/
    ├── dashboards/
    │   └── traces-overview.json         # Pre-built dashboard
    └── provisioning/
        ├── dashboards/
        │   └── default.yaml             # Dashboard provisioning
        └── datasources/
            └── graphite.yaml            # Datasource provisioning
```

## Building

### Prerequisites

- Go 1.21 or later
- Docker and Docker Compose (optional)

### From Source

```bash
git clone https://github.com/yourusername/gotel.git
cd gotel
make build
```

### Using Make

```bash
make deps      # Download dependencies
make build     # Build binary
make test      # Run tests
make run       # Build and run with config.yaml
make clean     # Remove build artifacts
```

## Running Tests

```bash
# Run all tests
go test -v ./...

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run specific test
go test -v -run TestTracesToMetrics ./exporter/graphiteexporter/
```

## Building Docker Image

```bash
# Build image
docker build -t gotel:latest .

# Run container
docker run -p 4317:4317 -p 4318:4318 -p 3200:3200 -p 8888:8888 gotel:latest
```

## Adding a New Exporter

1. Create a new directory under `exporter/`:
   ```
   exporter/myexporter/
   ├── factory.go
   ├── config.go
   └── exporter.go
   ```

2. Implement the `exporter.Factory` interface

3. Register in `main.go`:
   ```go
   factories.Exporters[myexporter.TypeStr] = myexporter.NewFactory()
   ```

## Architecture

```
┌─────────────────┐     ┌─────────────────────────────────────┐     ┌──────────────┐
│                 │     │              Gotel                  │     │              │
│  Your App       │────▶│  ┌─────────┐    ┌────────────────┐ │────▶│   Graphite   │
│  (OTLP Client)  │     │  │  OTLP   │───▶│    Graphite    │ │     │   (Carbon)   │
│                 │     │  │Receiver │    │    Exporter    │ │     │              │
└─────────────────┘     │  └─────────┘    └────────────────┘ │     └──────────────┘
                        └─────────────────────────────────────┘             │
                                                                           ▼
                                                                    ┌──────────────┐
                                                                    │   Grafana    │
                                                                    │  Dashboard   │
                                                                    └──────────────┘
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run tests: `make test`
5. Submit a pull request
