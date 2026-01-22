# Web UI - Built-in Trace Visualizer

Gotel includes a built-in web-based trace visualizer that uses PerfCascade for displaying trace data in a waterfall/Gantt chart format.

## Quick Start

```bash
docker-compose up -d
```

Open the Web UI at http://localhost:3000 to explore your trace data.

## Features

### Trace Explorer

- **Recent Traces Table**: View all recent traces with service, operation, duration, and timestamp
- **Trace Timeline (Gantt View)**: Interactive waterfall/Gantt visualization of selected trace spans
- **Span Details**: View attributes, events, status, and timing information for each span
- **Parent-Child Relationships**: Visual representation of span nesting and hierarchy

### Search and Filtering

- **Service Filter**: Filter traces by service name
- **Operation Filter**: Filter by span operation name
- **Duration Range**: Filter by minimum/maximum latency
- **Status Filter**: Show only successful or failed traces
- **Trace ID Search**: Direct search by trace ID (32 hex characters)

## Using the Web UI

### Browse Traces

1. Open the Web UI at http://localhost:3000
2. The **Recent Traces** table shows all available traces
3. Click on any trace to view its detailed timeline

### View Trace Timeline

1. Select a trace from the **Recent Traces** table
2. The **Trace Timeline** panel shows a Gantt-style waterfall view:
   - Horizontal bars represent span duration
   - Vertical positioning shows parent-child relationships
   - Color coding indicates span status (success/error)
   - Hover over spans to see detailed information

### Search by Trace ID

1. Enter the full trace ID (32 hex characters) in the search field
2. Click **Search** or press Enter
3. The trace timeline will display the selected trace

### Filter Traces

1. Use the filter controls to narrow down traces:
   - **Service**: Select from dropdown or enter service name
   - **Operation**: Filter by span name
   - **Duration**: Set min/max latency thresholds
   - **Status**: Choose OK, Error, or All

## API Endpoints Used by Web UI

The Web UI connects to the following endpoints on the Gotel query API (port 3200):

| Endpoint          | Method | Description                          |
| ----------------- | ------ | ------------------------------------ |
| `/api/traces`     | GET    | List all traces for the timeline     |
| `/api/spans`      | GET    | Get span details for visualization   |
| `/api/services`   | GET    | List available services for filtering|
| `/api/exceptions` | GET    | Get exception data for error analysis|

## HAR Format Conversion

The Web UI converts OpenTelemetry trace data to HAR (HTTP Archive) format for compatibility with PerfCascade:

### Trace to HAR Mapping

| OpenTelemetry Field | HAR Field                     | Description                          |
| ------------------- | ----------------------------- | ------------------------------------ |
| `trace_id`          | `_custom.trace_id`            | 32-hex trace identifier             |
| `span_id`           | `_custom.span_id`             | 16-hex span identifier              |
| `service_name`      | `request.url` (prefix)        | Service name                        |
| `span_name`         | `request.url` (suffix)        | Operation name                      |
| `start_time`        | `startedDateTime`             | Start timestamp                     |
| `duration_ms`       | `time`                        | Duration in milliseconds            |
| `status_code`       | `response.status`             | HTTP-like status code               |
| `parent_span_id`    | `_custom.parent_span_id`      | Parent span ID                      |

### Example HAR Entry

```json
{
  "startedDateTime": "2023-01-01T12:00:00.000Z",
  "time": 125,
  "request": {
    "method": "GET",
    "url": "api-gateway/GET__users",
    "httpVersion": "HTTP/1.1"
  },
  "response": {
    "status": 200,
    "statusText": "OK"
  },
  "_custom": {
    "trace_id": "5B8EFFF798038103D269B633813FC60C",
    "span_id": "EEE19B7EC3C1B174",
    "parent_span_id": "AABBCCDDEEFF0011",
    "service_name": "api-gateway",
    "span_name": "GET /users"
  }
}
```

## Troubleshooting

### Web UI not loading

1. Check if the web service is running: `docker-compose ps web`
2. Verify the web UI container logs: `docker-compose logs web`
3. Ensure the Gotel query API is accessible: `curl http://localhost:3200/api/status`

### No traces appearing

1. Verify traces are being received by Gotel: check main logs
2. Ensure `service.name` attribute is set in your application
3. Query the traces endpoint directly: `curl http://localhost:3200/api/traces`
4. Check if the web UI can connect to the query API

### Trace timeline not rendering

1. Verify the trace data is valid JSON
2. Check browser console for JavaScript errors
3. Ensure PerfCascade CSS is loading correctly
4. Try refreshing the page or clearing browser cache

### Slow performance with many traces

1. Reduce the time range or apply filters
2. Check database size: `ls -lh /data/gotel.db`
3. Consider reducing retention period in config
4. Use the search functionality to find specific traces

## Advanced Usage

### Customizing the Visualization

The Web UI uses PerfCascade for visualization. You can customize:

- **Color schemes**: Modify the CSS to change span colors
- **Layout**: Adjust the timeline scaling and spacing
- **Details panel**: Customize what information is displayed

### Integration with Other Tools

The Web UI can be embedded in other applications:

- Use iframe embedding for dashboards
- Connect to the query API directly from your applications
- Export trace data as JSON for analysis

### Keyboard Shortcuts

- **Arrow keys**: Navigate between traces
- **Space**: Expand/collapse selected span
- **Enter**: View details of selected span
- **Esc**: Return to trace list

## Browser Compatibility

The Web UI supports modern browsers:

- Chrome 90+
- Firefox 88+
- Safari 14+
- Edge 90+

For best performance, use the latest version of Chrome or Firefox.