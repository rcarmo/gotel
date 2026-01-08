# Gotel Makefile
# Run 'make' or 'make help' to see available targets

BINARY     := gotel
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GO         := go
GOFLAGS    := -v
LDFLAGS    := -ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# Default target
.DEFAULT_GOAL := help

##@ General

.PHONY: help
help: ## Show this help message
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: deps
deps: ## Download and tidy dependencies
	$(GO) mod download
	$(GO) mod tidy

.PHONY: fmt
fmt: ## Format Go source files
	$(GO) fmt ./...

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: lint
lint: fmt vet ## Run all linters (fmt + vet + staticcheck if available)
	@which staticcheck > /dev/null && staticcheck ./... || echo "staticcheck not installed, skipping"

.PHONY: lint-install
lint-install: ## Install linting tools
	$(GO) install honnef.co/go/tools/cmd/staticcheck@latest

##@ Testing

.PHONY: test
test: ## Run tests
	$(GO) test $(GOFLAGS) ./...

.PHONY: test-short
test-short: ## Run tests (short mode)
	$(GO) test -short ./...

.PHONY: test-race
test-race: ## Run tests with race detector
	$(GO) test -race $(GOFLAGS) ./...

.PHONY: test-coverage
test-coverage: ## Run tests with coverage report
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"
	@$(GO) tool cover -func=coverage.out | tail -1

.PHONY: test-coverage-check
test-coverage-check: ## Run tests and fail if coverage < 50%
	$(GO) test -coverprofile=coverage.out ./...
	@coverage=$$($(GO) tool cover -func=coverage.out | grep total | awk '{print substr($$3, 1, length($$3)-1)}'); \
	echo "Total coverage: $$coverage%"; \
	if [ $$(echo "$$coverage < 50" | bc -l) -eq 1 ]; then \
		echo "Coverage $$coverage% is below 50% threshold"; \
		exit 1; \
	fi

.PHONY: bench
bench: ## Run benchmarks
	$(GO) test -bench=. -benchmem ./...

##@ Building

.PHONY: build
build: ## Build the binary
	$(GO) build $(LDFLAGS) -o $(BINARY) .

.PHONY: build-debug
build-debug: ## Build with debug symbols
	$(GO) build -o $(BINARY) .

.PHONY: install
install: ## Install binary to GOPATH/bin
	$(GO) install $(LDFLAGS) .

##@ Running

.PHONY: run
run: build ## Build and run with default config
	./$(BINARY) --config config.yaml

.PHONY: run-debug
run-debug: build-debug ## Build and run with debug logging
	./$(BINARY) --config config.yaml --set service.telemetry.logs.level=debug

##@ Docker

.PHONY: docker-build
docker-build: ## Build Docker image
	docker build -t $(BINARY):$(VERSION) -t $(BINARY):latest .

.PHONY: docker-run
docker-run: ## Run Docker container
	docker run -p 4317:4317 -p 4318:4318 -p 8888:8888 $(BINARY):latest

.PHONY: docker-up
docker-up: ## Start full stack with docker-compose
	docker-compose up -d

.PHONY: docker-down
docker-down: ## Stop docker-compose stack
	docker-compose down

.PHONY: docker-logs
docker-logs: ## Follow docker-compose logs
	docker-compose logs -f

##@ Cleanup

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY)
	rm -f coverage.out coverage.html
	rm -f *.log

.PHONY: clean-all
clean-all: clean ## Remove build artifacts and cached data
	$(GO) clean -cache -testcache
	docker-compose down -v 2>/dev/null || true

##@ Release

.PHONY: release-dry
release-dry: ## Show what would be released
	@echo "Version: $(VERSION)"
	@echo "Binary:  $(BINARY)"
	@echo "Build:   $(BUILD_TIME)"

.PHONY: release-build
release-build: clean lint test build ## Full release build (lint, test, build)
	@echo "Built $(BINARY) version $(VERSION)"
