# Sending Traces

This guide shows how to send OpenTelemetry traces to Gotel from various languages.

## Go Application

```go
package main

import (
    "context"
    "log"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

func initTracer() (*sdktrace.TracerProvider, error) {
    ctx := context.Background()

    // Connect to Gotel
    conn, err := grpc.DialContext(ctx, "localhost:4317",
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
    if err != nil {
        return nil, err
    }

    exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
    if err != nil {
        return nil, err
    }

    // Define service name (appears in metrics as otel.traces.<service_name>)
    res, _ := resource.New(ctx,
        resource.WithAttributes(
            semconv.ServiceName("my-api"),
            semconv.ServiceVersion("1.0.0"),
        ),
    )

    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter),
        sdktrace.WithResource(res),
    )

    otel.SetTracerProvider(tp)
    return tp, nil
}

func main() {
    tp, err := initTracer()
    if err != nil {
        log.Fatal(err)
    }
    defer tp.Shutdown(context.Background())

    // Create spans
    tracer := otel.Tracer("my-api")
    ctx, span := tracer.Start(context.Background(), "GET /users")
    // ... your code ...
    span.End()
}
```

## Python Application

```python
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.resources import Resource

# Configure the tracer
resource = Resource.create({"service.name": "my-python-service"})
provider = TracerProvider(resource=resource)

# Export to Gotel
otlp_exporter = OTLPSpanExporter(endpoint="localhost:4317", insecure=True)
provider.add_span_processor(BatchSpanProcessor(otlp_exporter))
trace.set_tracer_provider(provider)

# Create spans
tracer = trace.get_tracer(__name__)
with tracer.start_as_current_span("my-operation"):
    # ... your code ...
    pass
```

## Node.js Application

```javascript
const { NodeTracerProvider } = require("@opentelemetry/sdk-trace-node");
const {
  OTLPTraceExporter,
} = require("@opentelemetry/exporter-trace-otlp-grpc");
const { BatchSpanProcessor } = require("@opentelemetry/sdk-trace-base");
const { Resource } = require("@opentelemetry/resources");
const {
  SemanticResourceAttributes,
} = require("@opentelemetry/semantic-conventions");

const provider = new NodeTracerProvider({
  resource: new Resource({
    [SemanticResourceAttributes.SERVICE_NAME]: "my-node-service",
  }),
});

const exporter = new OTLPTraceExporter({
  url: "http://localhost:4317",
});

provider.addSpanProcessor(new BatchSpanProcessor(exporter));
provider.register();

// Create spans
const tracer = provider.getTracer("my-node-service");
const span = tracer.startSpan("my-operation");
// ... your code ...
span.end();
```

## cURL (OTLP HTTP)

```bash
curl -X POST http://localhost:4318/v1/traces \
  -H "Content-Type: application/json" \
  -d '{
    "resourceSpans": [{
      "resource": {
        "attributes": [{
          "key": "service.name",
          "value": {"stringValue": "test-service"}
        }]
      },
      "scopeSpans": [{
        "spans": [{
          "traceId": "5B8EFFF798038103D269B633813FC60C",
          "spanId": "EEE19B7EC3C1B174",
          "name": "test-span",
          "kind": 1,
          "startTimeUnixNano": "1544712660000000000",
          "endTimeUnixNano": "1544712661000000000",
          "status": {}
        }]
      }]
    }]
  }'
```

## API Endpoints

| Endpoint | Protocol  | Port | Path         |
| -------- | --------- | ---- | ------------ |
| gRPC     | OTLP/gRPC | 4317 | -            |
| HTTP     | OTLP/HTTP | 4318 | `/v1/traces` |
