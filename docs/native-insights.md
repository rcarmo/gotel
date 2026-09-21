# Native insights and investigations

Gotel computes operational views directly from retained SQLite spans. The UI remains Preact; it does not include Jaeger or require another metrics database.

## Query contract

The Bun server proxies these collector endpoints without changing their response bodies:

| GET endpoint | Result |
| --- | --- |
| `/api/insights` | Counts, latency distributions, time buckets, slow operations, error groups, service activity and agent usage |
| `/api/explore` | Complete trace summaries selected by a matching span; `has_more` signals the result limit |
| `/api/investigation` | Downloadable version 1 investigation JSON containing explicitly selected complete traces and query context |

All three endpoints accept:

| Parameter | Meaning |
| --- | --- |
| `from`, `to` | Integer Unix seconds; half-open interval `[from, to)`. Defaults to the last hour. Maximum window: 7 days. |
| `service`, `operation` | Exact names |
| `status=error` | Span status code 2 |
| `min_duration_ms`, `max_duration_ms` | Inclusive non-negative duration bounds |
| `attribute`, `value` | Exact span attribute key/value pair. Empty values are supported. Scalar values use SQLite text conversion. Resource attributes are not searched. |
| `scope=requests` | Default: server, consumer or root spans, including roots with an all-zero parent ID |
| `scope=all` | Every matching span |

Predicates must match the same span. Trace search then retrieves summaries over all retained spans in each matching trace, including other services and spans outside the window. Summary `start_time` uses Unix nanoseconds. The existing `/api/traces/{id}/spans` endpoint loads a complete trace for the timeline.

`/api/explore` accepts `limit=1..200` (default 100). When `has_more` is true, narrow the filters or window. There is no pagination cursor in this version.

`/api/investigation` requires 1–20 distinct lowercase hexadecimal `trace_id` parameters. A selected trace need not match the context filters, so traces opened directly can also be exported.

Invalid input returns HTTP 400. Oversized aggregation or export returns 422; a query timeout returns 504. Native queries have a four-second deadline. The responses use `Cache-Control: no-store`. There are no write/import endpoints.

The TypeScript response definitions are in [`web/insights-types.ts`](../web/insights-types.ts).

## Counts and latency

Each window reads at most 20,000 matching spans before scope selection. Exceeding that bound fails the current query; it never returns a partial aggregate. Narrow the time window or service filter to reduce the input. The legacy per-batch Graphite averages are not used for these views.

- Counts and duration sums use individual spans. A negative stored duration contributes zero.
- Rates divide count by the actual window or clipped bucket width. Error rates are fractions.
- p50, p95 and p99 use nearest-rank percentiles of the individual durations.
- Histograms have disjoint millisecond ranges: `[0,10)`, `[10,50)`, `[50,100)`, `[100,500)`, `[500,1000)`, `[1000,+∞)`.
- Buckets align to UTC epoch boundaries. Their width increases with the query window; the first and last bucket are clipped to that window. Empty buckets are included.
- Representative operation and bucket traces contain the slowest observed matching span.

These are observed-span metrics. Sampling, missing instrumentation and nested server/consumer spans affect their relationship to end-to-end request traffic. They are not corrected for sampling, and retransmitted duplicate spans are counted as stored.

The preceding equal-length window provides operation p95 comparisons and error recurrence counts. If that window exceeds the input bound, `comparison_available` is false and previous values are null. Missing historical data is not proof that an error has never occurred.

## Landing page

Error groups use service, operation and the first `exception.type` on an exception event, falling back to `Error status`. They include all matching error spans, including internal failures excluded by request metrics. Status code 2 is required. Messages and stack traces are not grouping keys. The representative trace is the most recently observed member; versions, first/last occurrence and previous-window counts provide context. Version co-occurrence does not establish that a deployment caused the error.

Slow operations are ranked by current p95 with their previous-window p95 and percentage change. The landing page shows the top eight error groups and ten slow operations. Service activity uses only the service filter and time window, so an error-only search cannot make a working service appear inactive. It includes retained services outside the selected window and reports maximum span event time. Displayed versions come from spans matching the full query. Last-seen time does not measure uptime or ingestion health.

The UI starts with a 24-hour window. Relative presets advance on Refresh; custom windows remain fixed. Bucket selection opens trace search. Trace detail links use `#view=timeline&trace=<id>`.

## Agent and LLM interpretation

The optional Agents tab reads recognised attributes from every matching agent span, regardless of request scope. It groups by service, provider, model and operation. It does not infer model calls from span names alone.

Attribute precedence (first valid value wins):

| Field | Attributes |
| --- | --- |
| Provider | `gen_ai.provider.name`, `gen_ai.system` |
| Model | `gen_ai.response.model`, `gen_ai.request.model`, `llm.model_name` |
| Operation | `gen_ai.operation.name`, then the span name |
| Input tokens | `gen_ai.usage.input_tokens`, `gen_ai.usage.prompt_tokens`, `llm.usage.prompt_tokens` |
| Output tokens | `gen_ai.usage.output_tokens`, `gen_ai.usage.completion_tokens`, `llm.usage.completion_tokens` |
| Cost in USD | `gen_ai.usage.cost_usd`, `gen_ai.cost.total`, `llm.cost.total` |
| Explicit retry count | `gen_ai.request.retry_count`, `retry.count` |

The cost and retry aliases are Gotel conventions, not a claim that all instrumentation emits them. Cost fields must already be in USD. Gotel does not apply a price list or estimate missing costs. Input/output token coverage, cost coverage and retry coverage distinguish unknown values from measured zero. Numeric values must be finite, non-negative and at most 10^15; numeric strings are accepted. Alias values on one span are not added together. Parent/child usage reported twice by instrumentation remains two observations.

`gen_ai.tool.name` or an operation of `execute_tool`/`tool` identifies tool spans. Tool failures use status code 2. Repeated spans alone do not establish a retry. Prompts and response bodies are omitted from this view.

## Portable investigations

Export from selected trace-search rows or **Export this trace** on the timeline. The bundle includes:

- format/version, creation time, redaction policy and query filters;
- up to 20 complete selected traces, with a combined limit of 20,000 spans;
- captured aggregate metrics, error groups, service activity and agent usage for the context window.

Raw selected trace data and the final encoded bundle must each fit within 32 MiB. Complete means all spans retained at export time; Gotel cannot recover spans never ingested or already removed by retention. Selected traces are read in one SQLite snapshot. Context queries run separately and may observe later ingestion. Metric/error representatives in the context can refer to traces not selected for export.

The export allowlist preserves trace/span/parent IDs, timestamps, duration, status code, service/operation/kind labels, selected service/version/environment resource labels, recognised agent attributes and link IDs. Nanosecond timestamps are decimal strings. It omits messages, stacks, events, prompts/responses, arbitrary attributes, baggage and instrumentation scope metadata. The query's `filters.value` field is replaced with `[REDACTED]`, including in HTTP query logs. That does not remove a matching value from a retained model/provider/operation label or recognised agent attribute.

**Review bundles before sharing.** Service/operation/model/version labels, exception types and IDs remain visible. Labels can themselves contain sensitive data; this is payload removal, not anonymisation. Stored telemetry and the existing raw trace API are unchanged.

**Import bundle** validates the version, IDs, timestamps, types and size bounds before opening the JSON locally. It never uploads data or writes to SQLite. The UI disables live filters and makes no new API requests while inspecting a bundle. Late responses from prior live requests cannot replace imported evidence. Initial trace search lists every included trace; bucket drilldowns match the timestamps of any included span. They do not reapply removed attributes. The captured aggregate context remains fixed. A link to a trace absent from the bundle reports that absence locally. **Exit import** restores live queries.

## Refinement and acceptance

The target user operates a small instrumented deployment and needs an actionable opening screen, metric-to-trace navigation and portable evidence. The implementation covers the existing Go/SQLite backend, Bun proxy, Preact views, tests and documentation without adding runtime dependencies or changing the stored schema.

Existing ingestion, Tempo-style endpoints, Graphite queries and complete raw trace retrieval remain available. Jaeger integration, authentication, metric-only OTLP ingestion, disk budgets, pinning, new retention policies, pricing tables and durable incident storage are outside this change. Exports provide an independent evidence file after routine cleanup.

Acceptance checks cover invalid inputs, same-span filtering, full multi-service summaries, unequal duration observations, histogram boundaries, previous-window overflow, stale services, missing agent usage, exact export timestamps, payload removal and local import isolation. The smoke test ingests a 125-span trace and passes the real backend bundle through frontend validation. The optional browser test covers navigation, download, timeline rendering, bucket drilldown and a delayed live response during import.
