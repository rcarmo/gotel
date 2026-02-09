package sqliteexporter

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

func groupSpansAsOTLPResourceSpans(spans []json.RawMessage) []interface{} {
	// Group by resource.service.name (fallback to service_name) and scope.name.
	type scopeKey struct {
		service string
		scope   string
	}
	resources := make(map[string]map[string][]map[string]interface{})
	resourceAttrs := make(map[string][]map[string]interface{})
	scopeAttrs := make(map[scopeKey]map[string]interface{})

	for _, raw := range spans {
		var m map[string]interface{}
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}

		service := ""
		if res, ok := m["resource"].(map[string]interface{}); ok {
			if v, ok := res["service.name"].(string); ok {
				service = v
			}
			if service != "" {
				if _, exists := resourceAttrs[service]; !exists {
					resourceAttrs[service] = mapToOTLPAttributes(res)
				}
			}
		}
		if service == "" {
			if v, ok := m["service_name"].(string); ok {
				service = v
			}
		}
		if service == "" {
			service = "unknown"
		}

		scopeName := ""
		if scope, ok := m["scope"].(map[string]interface{}); ok {
			if v, ok := scope["name"].(string); ok {
				scopeName = v
			}
			if _, exists := scopeAttrs[scopeKey{service: service, scope: scopeName}]; !exists {
				scopeAttrs[scopeKey{service: service, scope: scopeName}] = map[string]interface{}{
					"name": scopeName,
				}
			}
		}

		if _, ok := resources[service]; !ok {
			resources[service] = make(map[string][]map[string]interface{})
		}

		otlpSpan := toOTLPSpan(m)
		resources[service][scopeName] = append(resources[service][scopeName], otlpSpan)
	}

	var out []interface{}
	for service, scopes := range resources {
		var scopeSpans []interface{}
		for scopeName, spanList := range scopes {
			scopeSpans = append(scopeSpans, map[string]interface{}{
				"scope": scopeAttrs[scopeKey{service: service, scope: scopeName}],
				"spans": spanList,
			})
		}

		out = append(out, map[string]interface{}{
			"resource": map[string]interface{}{
				"attributes": resourceAttrs[service],
			},
			"scopeSpans": scopeSpans,
		})
	}

	return out
}

func toOTLPSpan(m map[string]interface{}) map[string]interface{} {
	traceID, _ := m["trace_id"].(string)
	spanID, _ := m["span_id"].(string)
	parentSpanID, _ := m["parent_span_id"].(string)
	name, _ := m["span_name"].(string)
	kind, _ := m["kind"].(string)

	start := fmt.Sprintf("%v", m["start_time_unix_nano"])
	end := fmt.Sprintf("%v", m["end_time_unix_nano"])

	attrs := []map[string]interface{}{}
	if a, ok := m["attributes"].(map[string]interface{}); ok {
		attrs = mapToOTLPAttributes(a)
	}

	status := map[string]interface{}{}
	if st, ok := m["status"].(map[string]interface{}); ok {
		code := "STATUS_CODE_UNSET"
		if c, ok := st["code"].(float64); ok {
			switch int(c) {
			case 1:
				code = "STATUS_CODE_OK"
			case 2:
				code = "STATUS_CODE_ERROR"
			}
		}
		status["code"] = code
		if msg, ok := st["message"].(string); ok && msg != "" {
			status["message"] = msg
		}
	}

	otlpKind := "SPAN_KIND_UNSPECIFIED"
	switch strings.ToLower(kind) {
	case "internal":
		otlpKind = "SPAN_KIND_INTERNAL"
	case "server":
		otlpKind = "SPAN_KIND_SERVER"
	case "client":
		otlpKind = "SPAN_KIND_CLIENT"
	case "producer":
		otlpKind = "SPAN_KIND_PRODUCER"
	case "consumer":
		otlpKind = "SPAN_KIND_CONSUMER"
	}

	out := map[string]interface{}{
		"traceId":           traceID,
		"spanId":            spanID,
		"name":              name,
		"kind":              otlpKind,
		"startTimeUnixNano": start,
		"endTimeUnixNano":   end,
		"attributes":        attrs,
		"status":            status,
	}
	if parentSpanID != "" && parentSpanID != "0000000000000000" {
		out["parentSpanId"] = parentSpanID
	}

	if evs, ok := m["events"].([]interface{}); ok {
		converted := make([]map[string]interface{}, 0, len(evs))
		for _, ev := range evs {
			em, ok := ev.(map[string]interface{})
			if !ok {
				continue
			}
			ce := map[string]interface{}{}
			if n, ok := em["name"].(string); ok {
				ce["name"] = n
			}
			if ts, ok := em["timestamp"].(float64); ok {
				ce["timeUnixNano"] = fmt.Sprintf("%d", int64(ts))
			}
			if at, ok := em["attributes"].(map[string]interface{}); ok {
				ce["attributes"] = mapToOTLPAttributes(at)
			}
			converted = append(converted, ce)
		}
		if len(converted) > 0 {
			out["events"] = converted
		}
	}

	return out
}

func mapToOTLPAttributes(m map[string]interface{}) []map[string]interface{} {
	attrs := make([]map[string]interface{}, 0, len(m))
	for k, v := range m {
		attrs = append(attrs, map[string]interface{}{
			"key":   k,
			"value": toOTLPAnyValue(v),
		})
	}
	sort.Slice(attrs, func(i, j int) bool { return attrs[i]["key"].(string) < attrs[j]["key"].(string) })
	return attrs
}

func toOTLPAnyValue(v interface{}) map[string]interface{} {
	switch t := v.(type) {
	case string:
		return map[string]interface{}{"stringValue": t}
	case bool:
		return map[string]interface{}{"boolValue": t}
	case float64:
		// JSON numbers decode as float64.
		if math.Mod(t, 1) == 0 {
			return map[string]interface{}{"intValue": fmt.Sprintf("%d", int64(t))}
		}
		return map[string]interface{}{"doubleValue": t}
	case float32:
		return map[string]interface{}{"doubleValue": float64(t)}
	case int:
		return map[string]interface{}{"intValue": fmt.Sprintf("%d", t)}
	case int64:
		return map[string]interface{}{"intValue": fmt.Sprintf("%d", t)}
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return map[string]interface{}{"intValue": fmt.Sprintf("%d", i)}
		}
		if f, err := t.Float64(); err == nil {
			return map[string]interface{}{"doubleValue": f}
		}
		return map[string]interface{}{"stringValue": t.String()}
	default:
		return map[string]interface{}{"stringValue": fmt.Sprintf("%v", v)}
	}
}
